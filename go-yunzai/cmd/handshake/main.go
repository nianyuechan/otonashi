package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const snowLumaEndpoint = "ws://127.0.0.1:3001/"

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr, snowLumaEndpoint))
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer, endpoint string) int {
	token := getenv("SNOWLUMA_TOKEN")
	if strings.TrimSpace(token) == "" {
		fmt.Fprintln(stderr, "未设置 SNOWLUMA_TOKEN；请先在当前终端临时设置该环境变量。")
		return 2
	}
	safeStdout := &redactingWriter{writer: stdout, secret: token}
	safeStderr := &redactingWriter{writer: stderr, secret: token}

	flags := flag.NewFlagSet("handshake", flag.ContinueOnError)
	flags.SetOutput(safeStderr)
	sendSelf := flags.Bool("send-self-test", false, "仅向 get_login_info 返回的机器人自身账号发送测试私聊")
	targetQQ := flags.Int64("target-qq", 0, "可选目标 QQ；必须等于机器人自身 user_id")
	authMode := flags.String("auth-mode", "header", "鉴权方式：header（Bearer）或 query（access_token）")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(safeStderr, "不接受位置参数；token 只能来自 SNOWLUMA_TOKEN。")
		return 2
	}
	if *targetQQ != 0 && !*sendSelf {
		fmt.Fprintln(safeStderr, "-target-qq 只能与 -send-self-test 一起使用。")
		return 2
	}

	header := make(http.Header)
	dialURL := endpoint
	authDescription := "authorization=Bearer ***"
	switch *authMode {
	case "header":
		header.Set("Authorization", "Bearer "+token)
	case "query":
		parsed, err := url.Parse(endpoint)
		if err != nil {
			fmt.Fprintf(safeStderr, "WS 地址无效：%v\n", err)
			return 2
		}
		query := parsed.Query()
		query.Set("access_token", token)
		parsed.RawQuery = query.Encode()
		dialURL = parsed.String()
		authDescription = "access_token=***"
	default:
		fmt.Fprintln(safeStderr, "-auth-mode 只支持 header 或 query。")
		return 2
	}

	dialCtx, cancelDial := context.WithTimeout(ctx, 10*time.Second)
	conn, handshakeResponse, err := websocket.Dial(dialCtx, dialURL, &websocket.DialOptions{HTTPHeader: header})
	cancelDial()
	if err != nil {
		fmt.Fprintf(safeStderr, "WS 握手失败：%v\n", err)
		if handshakeResponse != nil && handshakeResponse.StatusCode == http.StatusUnauthorized {
			fmt.Fprintln(safeStderr, "SnowLuma 对缺少认证头、格式错误和 token 不匹配统一返回 401，无法进一步区分；请核对 SNOWLUMA_TOKEN 与 wsServers[].accessToken 完全一致，或尝试 -auth-mode query。")
			if handshakeResponse.Body != nil {
				body, _ := io.ReadAll(io.LimitReader(handshakeResponse.Body, 4096))
				_ = handshakeResponse.Body.Close()
				if len(bytes.TrimSpace(body)) != 0 {
					fmt.Fprintf(safeStderr, "401 返回体：%s\n", bytes.TrimSpace(body))
				}
			}
		}
		return 1
	}
	defer conn.CloseNow()
	defer conn.Close(websocket.StatusNormalClosure, "done")
	fmt.Fprintf(safeStdout, "handshake ok: url=ws://127.0.0.1:3001/ %s message_format=array\n", authDescription)

	login, raw, err := callAction(ctx, conn, "get_login_info", map[string]any{})
	printRaw(safeStdout, "get_login_info", raw, err)
	if err != nil {
		return 1
	}
	var selfID int64
	loginOK := *login.Retcode == 0
	if loginOK {
		var data struct {
			UserID int64 `json:"user_id"`
		}
		if err := json.Unmarshal(login.Data, &data); err != nil || data.UserID <= 0 {
			fmt.Fprintf(safeStderr, "get_login_info 缺少有效 user_id：%v\n", err)
			loginOK = false
		} else {
			selfID = data.UserID
		}
	}

	status, raw, err := callAction(ctx, conn, "get_status", map[string]any{})
	printRaw(safeStdout, "get_status", raw, err)
	if err != nil {
		return 1
	}
	if !loginOK || *status.Retcode != 0 {
		fmt.Fprintln(safeStderr, "只读 action 返回非零 retcode 或无效数据；不发送测试消息。")
		return 1
	}
	if !*sendSelf {
		return 0
	}

	target := selfID
	if *targetQQ != 0 {
		target = *targetQQ
	}
	if err := validateSelfTarget(selfID, target); err != nil {
		fmt.Fprintln(safeStderr, err)
		return 2
	}
	params := map[string]any{
		"user_id": target,
		"message": []map[string]any{{
			"type": "text",
			"data": map[string]any{"text": "[go-yunzai] SnowLuma 自身握手测试"},
		}},
	}
	sent, raw, err := callAction(ctx, conn, "send_private_msg", params)
	printRaw(safeStdout, "send_private_msg", raw, err)
	if err != nil {
		return 1
	}
	if *sent.Retcode != 0 {
		return 1
	}
	return 0
}

type actionResponse struct {
	Retcode *int64          `json:"retcode"`
	Data    json.RawMessage `json:"data"`
	Echo    string          `json:"echo"`
}

func callAction(ctx context.Context, conn *websocket.Conn, action string, params any) (actionResponse, []byte, error) {
	echo := "handshake-" + action
	payload, err := json.Marshal(map[string]any{"action": action, "params": params, "echo": echo})
	if err != nil {
		return actionResponse{}, nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := conn.Write(callCtx, websocket.MessageText, payload); err != nil {
		return actionResponse{}, nil, err
	}
	for {
		_, raw, err := conn.Read(callCtx)
		if err != nil {
			return actionResponse{}, raw, err
		}
		var response actionResponse
		if err := json.Unmarshal(raw, &response); err != nil {
			return actionResponse{}, raw, err
		}
		if response.Echo != echo {
			continue
		}
		if response.Retcode == nil {
			return actionResponse{}, raw, fmt.Errorf("%s 响应缺少 retcode", action)
		}
		return response, raw, nil
	}
}

func printRaw(writer io.Writer, action string, raw []byte, err error) {
	if len(raw) != 0 {
		fmt.Fprintf(writer, "%s: %s\n", action, raw)
	}
	if err != nil {
		fmt.Fprintf(writer, "%s 调用失败：%v\n", action, err)
	}
}

func validateSelfTarget(selfID, target int64) error {
	if selfID <= 0 || target <= 0 || selfID != target {
		return fmt.Errorf("目标 QQ 与机器人自身不一致，拒绝发送")
	}
	return nil
}

type redactingWriter struct {
	writer io.Writer
	secret string
}

func (w *redactingWriter) Write(data []byte) (int, error) {
	originalLength := len(data)
	if w.secret != "" {
		data = bytes.ReplaceAll(data, []byte(w.secret), []byte("***"))
		escaped := url.QueryEscape(w.secret)
		if escaped != w.secret {
			data = bytes.ReplaceAll(data, []byte(escaped), []byte("***"))
		}
	}
	_, err := w.writer.Write(data)
	return originalLength, err
}

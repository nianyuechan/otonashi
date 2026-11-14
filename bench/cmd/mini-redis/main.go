// mini-redis 用 miniredis 在本机起一个 Redis 兼容服务，供旧版 TRSS-Yunzai
// 基准运行使用。只服务 127.0.0.1，重启后数据清空，禁止用于生产。
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/alicebob/miniredis/v2"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:6379", "监听地址")
	flag.Parse()

	server := miniredis.NewMiniRedis()
	// StartAddr 只绑定指定地址；miniredis 仅供本机基准使用。
	if err := server.StartAddr(*addr); err != nil {
		log.Fatalf("启动 miniredis 失败: %v", err)
	}
	fmt.Printf("mini-redis listening on %s\n", *addr)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	server.Close()
}

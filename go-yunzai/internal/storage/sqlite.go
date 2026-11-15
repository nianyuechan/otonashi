// Package storage implements durable, plugin-scoped state in a separate SQLite
// database. It does not open or migrate legacy production databases.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nianyuechan/otonashi/go-yunzai/pkg/plugin/sdk"
	_ "modernc.org/sqlite"
)

type SQLite struct {
	db        *sql.DB
	path      string
	now       func() time.Time
	closed    atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

func Open(ctx context.Context, path string) (*SQLite, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" || path == ":memory:" {
		return nil, fmt.Errorf("plugin store requires a persistent file path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, sdk.ErrStoreUnavailable
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, sdk.ErrStoreUnavailable
	}
	file, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, sdk.ErrStoreUnavailable
	}
	if err := file.Close(); err != nil {
		return nil, sdk.ErrStoreUnavailable
	}
	uriPath := filepath.ToSlash(abs)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath // Windows drive paths need file:///G:/...
	}
	dsn := url.URL{Scheme: "file", Path: uriPath}
	dsn.RawQuery = experimentPragmas().Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, sdk.ErrStoreUnavailable
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &SQLite{db: db, path: abs, now: time.Now}
	// Keep the default rollback journal. Switching journal_mode during every
	// process start requires an exclusive lock and made rapid restart compete
	// with a previous close; busy_timeout still protects ordinary writes.
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS yzgo_plugin_kv (
			namespace TEXT NOT NULL,
			key TEXT NOT NULL,
			value BLOB NOT NULL,
			expires_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (namespace, key)
		) WITHOUT ROWID`)
	if err != nil {
		_ = s.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, sdk.ErrStoreUnavailable
	}
	return s, nil
}

// experimentPragmas 返回连接 DSN 的 pragma 组合。默认保持 synchronous(FULL) +
// 回滚日志；设置 YZGO_STORAGE_EXPERIMENT=wal-normal 时改用 WAL +
// synchronous(NORMAL)，这是阶段零 Z2 的显式对照实验开关（2026-11-15）：默认
// FULL 在本机 G: 机械盘上实测只有 ~9 提交/秒，无法承载 200 msg/s 目标；WAL +
// NORMAL 实测 ~2 万提交/秒，代价是进程崩溃或断电时可能丢失最后若干未
// checkpoint 的事务（持久性换吞吐，默认不启用）。journal_mode(WAL) 在启动时
// 需要短暂独占锁；本开关只用于单实例基准实验，快速重启竞争场景不适用。
func experimentPragmas() url.Values {
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(2500)")
	if os.Getenv("YZGO_STORAGE_EXPERIMENT") == "wal-normal" {
		query.Add("_pragma", "journal_mode(WAL)")
		query.Add("_pragma", "synchronous(NORMAL)")
	} else {
		query.Add("_pragma", "synchronous(FULL)")
	}
	return query
}

// Scope binds a namespace using a separate SQL column and bound parameters;
// neither punctuation in a key nor in an id can escape into another plugin.
func (s *SQLite) Scope(pluginID string) sdk.KVStore {
	return scoped{s: s, namespace: "yzgo:" + pluginID + ":v1"}
}

// Path 返回打开时解析出的绝对数据库路径。
func (s *SQLite) Path() string { return s.path }

func (s *SQLite) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		if err := s.db.Close(); err != nil {
			s.closeErr = sdk.ErrStoreUnavailable
		}
	})
	return s.closeErr
}

type scoped struct {
	s         *SQLite
	namespace string
}

func (v scoped) validate(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.s.closed.Load() {
		return sdk.ErrStoreClosed
	}
	if len(key) == 0 || len(key) > 1024 {
		return sdk.ErrInvalidKey
	}
	return nil
}

func (v scoped) failure(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.s.closed.Load() {
		return sdk.ErrStoreClosed
	}
	// Avoid logging SQL error payloads, which may contain state values.
	return sdk.ErrStoreUnavailable
}

func (v scoped) Get(ctx context.Context, key string) ([]byte, error) {
	if err := v.validate(ctx, key); err != nil {
		return nil, err
	}
	var value []byte
	var expires int64
	err := v.s.db.QueryRowContext(ctx, "SELECT value, expires_at FROM yzgo_plugin_kv WHERE namespace=? AND key=?", v.namespace, key).Scan(&value, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sdk.ErrKeyNotFound
	}
	if err != nil {
		return nil, v.failure(ctx)
	}
	now := v.s.now().UnixMilli()
	if expires != 0 && expires <= now {
		// Do not delete a concurrent replacement with a later expiry or no TTL.
		_, err = v.s.db.ExecContext(ctx, "DELETE FROM yzgo_plugin_kv WHERE namespace=? AND key=? AND expires_at<>0 AND expires_at<=?", v.namespace, key, now)
		if err != nil {
			return nil, v.failure(ctx)
		}
		return nil, sdk.ErrKeyNotFound
	}
	return append([]byte{}, value...), nil
}

func (v scoped) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := v.validate(ctx, key); err != nil {
		return err
	}
	if ttl < 0 {
		return sdk.ErrInvalidTTL
	}
	var expires int64
	if ttl > 0 {
		until := v.s.now().Add(ttl)
		expires = until.UnixMilli()
		if until.Nanosecond()%int(time.Millisecond) != 0 {
			expires++ // Round up so sub-millisecond TTLs never expire early.
		}
	}
	if value == nil {
		value = []byte{}
	}
	_, err := v.s.db.ExecContext(ctx, `INSERT INTO yzgo_plugin_kv(namespace,key,value,expires_at) VALUES(?,?,?,?)
		ON CONFLICT(namespace,key) DO UPDATE SET value=excluded.value,expires_at=excluded.expires_at`, v.namespace, key, value, expires)
	if err != nil {
		return v.failure(ctx)
	}
	return nil
}

func (v scoped) Delete(ctx context.Context, key string) error {
	if err := v.validate(ctx, key); err != nil {
		return err
	}
	_, err := v.s.db.ExecContext(ctx, "DELETE FROM yzgo_plugin_kv WHERE namespace=? AND key=?", v.namespace, key)
	if err != nil {
		return v.failure(ctx)
	}
	return nil
}

package storage

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nianyuechan/otonashi/go-yunzai/pkg/plugin/sdk"
	"github.com/redis/go-redis/v9"
)

// RedisOptions describes one explicitly selected Redis endpoint. The adapter
// never discovers or rewrites legacy keys automatically.
type RedisOptions struct {
	Addr         string
	Username     string
	Password     string
	DB           int
	MaxRetries   int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Redis is a durable, plugin-scoped KV backend. go-redis reconnects between
// commands, while each command still observes its caller context. Values and
// operational errors are never returned as implicit empty data.
type Redis struct {
	client    *redis.Client
	closed    atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

func OpenRedis(ctx context.Context, options RedisOptions) (*Redis, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(options.Addr) == "" || options.MaxRetries < 0 {
		return nil, sdk.ErrStoreUnavailable
	}
	client := redis.NewClient(&redis.Options{
		Addr:         options.Addr,
		Username:     options.Username,
		Password:     options.Password,
		DB:           options.DB,
		MaxRetries:   options.MaxRetries,
		DialTimeout:  options.DialTimeout,
		ReadTimeout:  options.ReadTimeout,
		WriteTimeout: options.WriteTimeout,
	})
	backend := &Redis{client: client}
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, sdk.ErrStoreUnavailable
	}
	return backend, nil
}

func (r *Redis) Scope(pluginID string) sdk.KVStore {
	return redisScoped{backend: r, prefix: "yzgo:" + pluginID + ":v1:"}
}

func (r *Redis) Close() error {
	r.closeOnce.Do(func() {
		r.closed.Store(true)
		if err := r.client.Close(); err != nil {
			r.closeErr = sdk.ErrStoreUnavailable
		}
	})
	return r.closeErr
}

type redisScoped struct {
	backend *Redis
	prefix  string
}

func (s redisScoped) validate(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.backend.closed.Load() {
		return sdk.ErrStoreClosed
	}
	if len(key) == 0 || len(key) > 1024 {
		return sdk.ErrInvalidKey
	}
	return nil
}

func (s redisScoped) failure(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if s.backend.closed.Load() {
		return sdk.ErrStoreClosed
	}
	// Redis errors can echo key names or connection details; keep the SDK
	// boundary stable and leave redacted logging to the caller.
	_ = err
	return sdk.ErrStoreUnavailable
}

func (s redisScoped) Get(ctx context.Context, key string) ([]byte, error) {
	if err := s.validate(ctx, key); err != nil {
		return nil, err
	}
	value, err := s.backend.client.Get(ctx, s.prefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, sdk.ErrKeyNotFound
	}
	if err != nil {
		return nil, s.failure(ctx, err)
	}
	return append([]byte(nil), value...), nil
}

func (s redisScoped) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := s.validate(ctx, key); err != nil {
		return err
	}
	if ttl < 0 {
		return sdk.ErrInvalidTTL
	}
	if err := s.backend.client.Set(ctx, s.prefix+key, value, ttl).Err(); err != nil {
		return s.failure(ctx, err)
	}
	return nil
}

func (s redisScoped) Delete(ctx context.Context, key string) error {
	if err := s.validate(ctx, key); err != nil {
		return err
	}
	if err := s.backend.client.Del(ctx, s.prefix+key).Err(); err != nil {
		return s.failure(ctx, err)
	}
	return nil
}

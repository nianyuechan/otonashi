package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/nianyuechan/otonashi/go-yunzai/pkg/plugin/sdk"
	"github.com/redis/go-redis/v9"
)

// Legacy exposes raw Redis keys only to explicit compatibility and migration
// code. Ordinary plugins must use Redis.Scope and cannot select this adapter.
type Legacy struct {
	backend *Redis
}

func (r *Redis) Legacy() *Legacy { return &Legacy{backend: r} }

func (l *Legacy) validate(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if l == nil || l.backend == nil || l.backend.closed.Load() {
		return sdk.ErrStoreClosed
	}
	if len(key) == 0 || len(key) > 1024 {
		return sdk.ErrInvalidKey
	}
	return nil
}

func (l *Legacy) failure(ctx context.Context, err error, command bool) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if l.backend.closed.Load() {
		return sdk.ErrStoreClosed
	}
	if command {
		var redisErr redis.Error
		if errors.As(err, &redisErr) {
			return sdk.ErrStoreCommand
		}
	}
	_ = err
	return sdk.ErrStoreUnavailable
}

func (l *Legacy) Get(ctx context.Context, key string) ([]byte, error) {
	if err := l.validate(ctx, key); err != nil {
		return nil, err
	}
	value, err := l.backend.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, sdk.ErrKeyNotFound
	}
	if err != nil {
		return nil, l.failure(ctx, err, false)
	}
	return append([]byte(nil), value...), nil
}

func (l *Legacy) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := l.validate(ctx, key); err != nil {
		return err
	}
	if ttl < 0 {
		return sdk.ErrInvalidTTL
	}
	if err := l.backend.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return l.failure(ctx, err, true)
	}
	return nil
}

func (l *Legacy) Delete(ctx context.Context, key string) error {
	if err := l.validate(ctx, key); err != nil {
		return err
	}
	if err := l.backend.client.Del(ctx, key).Err(); err != nil {
		return l.failure(ctx, err, true)
	}
	return nil
}

func (l *Legacy) Incr(ctx context.Context, key string) (int64, error) {
	if err := l.validate(ctx, key); err != nil {
		return 0, err
	}
	value, err := l.backend.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, l.failure(ctx, err, true)
	}
	return value, nil
}

func (l *Legacy) TTL(ctx context.Context, key string) (time.Duration, error) {
	if err := l.validate(ctx, key); err != nil {
		return 0, err
	}
	value, err := l.backend.client.TTL(ctx, key).Result()
	if err != nil {
		return 0, l.failure(ctx, err, false)
	}
	if value == -2 {
		return 0, sdk.ErrKeyNotFound
	}
	if value == -1 {
		return 0, nil
	}
	return value, nil
}

type LegacyOpKind uint8

const (
	LegacyGet LegacyOpKind = iota
	LegacySet
	LegacyDelete
	LegacyIncr
)

type LegacyOp struct {
	Kind  LegacyOpKind
	Key   string
	Value []byte
	TTL   time.Duration
}

type LegacyResult struct {
	Value  []byte
	Number int64
	Err    error
}

// Batch deliberately uses a Redis pipeline, not MULTI/EXEC. Each command has
// an independent result, so a wrong-type or other command failure is visible
// without being silently converted into an all-or-nothing transaction.
func (l *Legacy) Batch(ctx context.Context, operations ...LegacyOp) ([]LegacyResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if l == nil || l.backend == nil || l.backend.closed.Load() {
		return nil, sdk.ErrStoreClosed
	}
	commands := make([]redis.Cmder, len(operations))
	pipeline := l.backend.client.Pipeline()
	for i, operation := range operations {
		if err := l.validate(ctx, operation.Key); err != nil {
			return nil, err
		}
		if operation.Kind == LegacySet && operation.TTL < 0 {
			return nil, sdk.ErrInvalidTTL
		}
		switch operation.Kind {
		case LegacyGet:
			commands[i] = pipeline.Get(ctx, operation.Key)
		case LegacySet:
			commands[i] = pipeline.Set(ctx, operation.Key, operation.Value, operation.TTL)
		case LegacyDelete:
			commands[i] = pipeline.Del(ctx, operation.Key)
		case LegacyIncr:
			commands[i] = pipeline.Incr(ctx, operation.Key)
		default:
			return nil, sdk.ErrStoreCommand
		}
	}
	_, execErr := pipeline.Exec(ctx)
	results := make([]LegacyResult, len(commands))
	commandFailed := false
	backendFailed := false
	for i, command := range commands {
		if err := command.Err(); err != nil {
			if errors.Is(err, redis.Nil) && operations[i].Kind == LegacyGet {
				results[i].Err = sdk.ErrKeyNotFound
			} else {
				results[i].Err = l.failure(ctx, err, true)
				if errors.Is(results[i].Err, sdk.ErrStoreUnavailable) {
					backendFailed = true
				}
			}
			commandFailed = true
			continue
		}
		switch typed := command.(type) {
		case *redis.StringCmd:
			results[i].Value, _ = typed.Bytes()
		case *redis.IntCmd:
			results[i].Number, _ = typed.Result()
		}
	}
	if execErr != nil && (!commandFailed || backendFailed) {
		return results, l.failure(ctx, execErr, false)
	}
	return results, nil
}

type LegacyRecord struct {
	Key       string
	Exists    bool
	Bytes     int
	SHA256    string
	JSONValid bool
	TTL       time.Duration
}

// Inspect reads an explicit list of legacy keys and returns hashes and TTLs,
// never raw values. It performs no writes and is suitable for copied-data
// migration reports.
func (l *Legacy) Inspect(ctx context.Context, keys ...string) ([]LegacyRecord, error) {
	records := make([]LegacyRecord, len(keys))
	for i, key := range keys {
		if err := l.validate(ctx, key); err != nil {
			return nil, err
		}
		records[i].Key = key
		value, err := l.Get(ctx, key)
		if errors.Is(err, sdk.ErrKeyNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		records[i].Exists = true
		records[i].Bytes = len(value)
		hash := sha256.Sum256(value)
		records[i].SHA256 = hex.EncodeToString(hash[:])
		records[i].JSONValid = json.Valid(value)
		records[i].TTL, err = l.TTL(ctx, key)
		if err != nil {
			return nil, err
		}
	}
	return records, nil
}

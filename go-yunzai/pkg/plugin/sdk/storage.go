package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrKeyNotFound      = errors.New("plugin key not found")
	ErrStoreUnavailable = errors.New("plugin store unavailable")
	ErrStoreCommand     = errors.New("plugin store command failed")
	ErrStoreClosed      = errors.New("plugin store closed")
	ErrInvalidKey       = errors.New("plugin key must contain 1 to 1024 bytes")
	ErrInvalidTTL       = errors.New("plugin ttl must be non-negative and representable")
)

// KVStore is scoped to one plugin. Keys and values are opaque; callers cannot
// select another plugin's namespace. Set replaces the value and TTL atomically:
// zero persists indefinitely, positive expires, negative is invalid. Get of a
// missing or expired key returns ErrKeyNotFound; Delete is idempotent. Returned
// bytes belong to the caller. Errors never imply a successful read of no data.
// Handles are revoked when the owning plugin stops; persisted data is retained.
type KVStore interface {
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, string) error
}

// ConfigView is an immutable snapshot of this plugin's settings, excluding
// core credentials. Get returns a copy of the JSON value. Every phase of an
// event sees the same revision; tasks get a new snapshot on each invocation.
type ConfigView interface {
	Revision() uint64
	Get(key string) (json.RawMessage, bool)
}

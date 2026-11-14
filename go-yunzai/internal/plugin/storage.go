package plugin

import (
	"context"
	"time"

	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
)

// StoreProvider is an internal application dependency. Plugins receive only
// the scoped KVStore, never the factory that can select other namespaces.
type StoreProvider interface {
	Scope(pluginID string) sdk.KVStore
}

type runtimeStore struct {
	lifetime context.Context
	backend  sdk.KVStore
}

func (m *Manager) storeFor(pluginID string, run *pluginRun) sdk.KVStore {
	var backend sdk.KVStore
	if m.stores != nil {
		backend = m.stores.Scope(pluginID)
	}
	return runtimeStore{lifetime: run.ctx, backend: backend}
}

func (s runtimeStore) operation(ctx context.Context) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if s.lifetime.Err() != nil {
		return nil, nil, sdk.ErrNotRunning
	}
	if s.backend == nil {
		return nil, nil, sdk.ErrStoreUnavailable
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.lifetime, cancel)
	if s.lifetime.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }, nil
}

func (s runtimeStore) Get(ctx context.Context, key string) ([]byte, error) {
	ctx, cancel, err := s.operation(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.backend.Get(ctx, key)
}

func (s runtimeStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	ctx, cancel, err := s.operation(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	return s.backend.Set(ctx, key, value, ttl)
}

func (s runtimeStore) Delete(ctx context.Context, key string) error {
	ctx, cancel, err := s.operation(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	return s.backend.Delete(ctx, key)
}

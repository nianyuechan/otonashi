package storage

import "github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"

// Backend is the application-level storage dependency. Scope is the only
// operation exposed to plugins; lifecycle remains owned by the application.
type Backend interface {
	Scope(string) sdk.KVStore
	Close() error
}

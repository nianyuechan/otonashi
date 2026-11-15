// Package pluginconfig publishes validated, immutable plugin settings.
package pluginconfig

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/nianyuechan/otonashi/go-yunzai/pkg/plugin/sdk"
)

// Layers uses shallow field replacement. Groups are applied in legacy order:
// default, <account>:default, <group>, <account>:<group>.
type Layers struct {
	Defaults map[string]any            `yaml:"defaults"`
	User     map[string]any            `yaml:"user"`
	Groups   map[string]map[string]any `yaml:"groups"`
}

type values map[string]json.RawMessage
type frozenLayers struct {
	base   values
	groups map[string]values
}

type Snapshot struct {
	revision uint64
	plugins  map[string]frozenLayers
}

type Registry struct {
	mu      sync.Mutex
	current atomic.Pointer[Snapshot]
}

func New(settings map[string]Layers) (*Registry, error) {
	r := &Registry{}
	if err := r.Replace(settings); err != nil {
		return nil, err
	}
	return r, nil
}

// Replace validates and copies everything before publishing. Failure keeps the
// previous revision; neither caller mutations nor returned values can alter it.
func (r *Registry) Replace(settings map[string]Layers) error {
	next := &Snapshot{plugins: make(map[string]frozenLayers, len(settings))}
	for id, layers := range settings {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("plugin settings require a plugin id")
		}
		base := make(values)
		if err := merge(base, layers.Defaults); err != nil {
			return err
		}
		if err := merge(base, layers.User); err != nil {
			return err
		}
		groups := make(map[string]values, len(layers.Groups))
		for group, fields := range layers.Groups {
			if !validGroup(group) {
				return fmt.Errorf("plugin settings contain an invalid group selector")
			}
			groups[group] = make(values)
			if err := merge(groups[group], fields); err != nil {
				return err
			}
		}
		next.plugins[id] = frozenLayers{base: base, groups: groups}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next.revision = r.Snapshot().revision + 1
	r.current.Store(next)
	return nil
}

func validGroup(selector string) bool {
	parts := strings.Split(selector, ":")
	if len(parts) > 2 {
		return false
	}
	for i, part := range parts {
		if part == "default" && i == len(parts)-1 {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != part {
			return false
		}
	}
	return true
}

func merge(dst values, src map[string]any) error {
	for key, value := range src {
		if key == "" {
			return fmt.Errorf("plugin setting key must not be empty")
		}
		data, err := json.Marshal(value)
		if err != nil {
			// Do not include values or serialization error payloads in logs.
			return fmt.Errorf("plugin settings must be JSON serializable")
		}
		dst[key] = data
	}
	return nil
}

func (r *Registry) Snapshot() *Snapshot {
	if current := r.current.Load(); current != nil {
		return current
	}
	return &Snapshot{}
}

func (s *Snapshot) View(pluginID string, accountID, groupID int64) sdk.ConfigView {
	layers := s.plugins[pluginID]
	fields := make(values, len(layers.base))
	copyFields(fields, layers.base)
	if groupID > 0 {
		account, group := strconv.FormatInt(accountID, 10), strconv.FormatInt(groupID, 10)
		for _, selector := range []string{"default", account + ":default", group, account + ":" + group} {
			copyFields(fields, layers.groups[selector])
		}
	}
	return view{revision: s.revision, fields: fields}
}

func copyFields(dst, src values) {
	for key, value := range src {
		dst[key] = value
	}
}

type view struct {
	revision uint64
	fields   values
}

func (v view) Revision() uint64 { return v.revision }

func (v view) Get(key string) (json.RawMessage, bool) {
	value, ok := v.fields[key]
	return append(json.RawMessage(nil), value...), ok
}

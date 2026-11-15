package pluginconfig

import (
	"fmt"
	"sync"
	"testing"

	"github.com/nianyuechan/otonashi/go-yunzai/pkg/plugin/sdk"
)

func requireValue(t *testing.T, view sdk.ConfigView, key, want string) {
	t.Helper()
	got, ok := view.Get(key)
	if !ok || string(got) != want {
		t.Fatalf("%s = %s, %v; want %s", key, got, ok, want)
	}
}

func TestShallowMergeLegacyGroupOrderAndIsolation(t *testing.T) {
	input := map[string]Layers{"a": {
		Defaults: map[string]any{"nested": map[string]any{"old": 1, "keep": 2}, "base": "default"},
		User:     map[string]any{"nested": map[string]any{"new": 3}},
		Groups: map[string]map[string]any{
			"default":    {"rank": "default", "one": 1},
			"10:default": {"rank": "account", "two": 2},
			"30":         {"rank": "group", "three": 3},
			"10:30":      {"rank": "specific", "four": 4},
		},
	}, "b": {Defaults: map[string]any{"base": "other"}}}
	r, err := New(input)
	if err != nil {
		t.Fatal(err)
	}
	view := r.Snapshot().View("a", 10, 30)
	requireValue(t, view, "nested", `{"new":3}`)
	requireValue(t, view, "rank", `"specific"`)
	for i, key := range []string{"one", "two", "three", "four"} {
		requireValue(t, view, key, fmt.Sprint(i+1))
	}
	requireValue(t, r.Snapshot().View("a", 11, 30), "rank", `"group"`)
	requireValue(t, r.Snapshot().View("a", 10, 31), "rank", `"account"`)
	requireValue(t, r.Snapshot().View("a", 11, 31), "rank", `"default"`)
	if _, ok := r.Snapshot().View("a", 10, 0).Get("rank"); ok {
		t.Fatal("private chat received group settings")
	}
	requireValue(t, r.Snapshot().View("b", 10, 30), "base", `"other"`)
	if _, ok := r.Snapshot().View("missing", 10, 30).Get("base"); ok {
		t.Fatal("unknown plugin leaked another plugin's settings")
	}
	input["a"].Groups["10:30"]["rank"] = "mutated"
	input["a"].User["nested"].(map[string]any)["new"] = 9
	raw, _ := view.Get("rank")
	raw[0] = 'x'
	requireValue(t, view, "rank", `"specific"`)
	requireValue(t, view, "nested", `{"new":3}`)
}

func TestReplacementIsAtomicAndInvalidUpdateKeepsRevision(t *testing.T) {
	r, err := New(map[string]Layers{"a": {Defaults: map[string]any{"x": 1, "y": 1}}})
	if err != nil {
		t.Fatal(err)
	}
	old := r.Snapshot().View("a", 0, 0)
	for _, settings := range []map[string]Layers{
		{"a": {User: map[string]any{"secret": func() {}}}},
		{"a": {Groups: map[string]map[string]any{"default:30": {}}}},
		{"a": {Groups: map[string]map[string]any{"10:0": {}}}},
		{"a": {User: map[string]any{"": 1}}},
		{"": {}},
	} {
		if err := r.Replace(settings); err == nil {
			t.Fatal("invalid replacement accepted")
		}
		if r.Snapshot().View("a", 0, 0).Revision() != old.Revision() {
			t.Fatal("failed update changed revision")
		}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 100 {
			if err := r.Replace(map[string]Layers{"a": {User: map[string]any{"x": i, "y": i}}}); err != nil {
				t.Error(err)
			}
		}
	}()
	for range 200 {
		view := r.Snapshot().View("a", 0, 0)
		x, _ := view.Get("x")
		y, _ := view.Get("y")
		if string(x) != string(y) {
			t.Fatalf("partial config revision: x=%s y=%s", x, y)
		}
	}
	wg.Wait()
	requireValue(t, old, "x", "1")
	if r.Snapshot().View("a", 0, 0).Revision() <= old.Revision() {
		t.Fatal("successful update did not advance revision")
	}
}

package keymap

import (
	"reflect"
	"testing"
)

func TestCanonicalKey(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"q":          "q",
		"Q":          "Q",
		" ":          "space",
		" escape ":   "esc",
		"Return":     "enter",
		"PageUp":     "pgup",
		"ctrl+S":     "ctrl+s",
		"alt+X":      "alt+x",
		"Shift+Left": "shift+left",
		"F2":         "f2",
		"":           "",
	}
	for raw, want := range cases {
		if got := CanonicalKey(raw); got != want {
			t.Errorf("CanonicalKey(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestParseKey(t *testing.T) {
	t.Parallel()

	valid := []string{"q", "Q", "space", "esc", "enter", "tab", "up", "f2", "ctrl+c", "alt+x", "shift+tab", "shift+left"}
	for _, raw := range valid {
		if _, err := ParseKey(raw); err != nil {
			t.Errorf("ParseKey(%q) returned %v", raw, err)
		}
	}

	invalid := []string{"", "   ", "ctrl+", "ctrl+ab", "shift+x", "not-a-key", "\n"}
	for _, raw := range invalid {
		if _, err := ParseKey(raw); err == nil {
			t.Errorf("ParseKey(%q) must fail", raw)
		}
	}
}

func TestDefaultRegistryCoversEveryScope(t *testing.T) {
	t.Parallel()

	keys := Default()
	for _, scope := range []Scope{ScopeRoot, ScopePorts, ScopeInspector, ScopeFilter} {
		if len(keys.Bindings(scope)) == 0 {
			t.Errorf("scope %q has no bindings", scope)
		}
	}
}

func TestRegistryMatch(t *testing.T) {
	t.Parallel()

	keys := Default()

	cases := []struct {
		scope Scope
		key   string
		want  Action
	}{
		{ScopeRoot, "q", ActionQuit},
		{ScopeRoot, "1", ActionTabListen},
		{ScopeRoot, "2", ActionTabConnected},
		{ScopeRoot, "3", ActionTabAll},
		{ScopeRoot, "tab", ActionFocusNext},
		{ScopePorts, "j", ActionPortsDown},
		{ScopePorts, "x", ActionPortsKill},
		{ScopePorts, "t", ActionPortsTerm},
		{ScopePorts, "/", ActionPortsFilter},
		{ScopePorts, "s", ActionPortsSort},
		{ScopeInspector, "k", ActionInspectorUp},
		{ScopeFilter, "enter", ActionFilterApply},
	}
	for _, tc := range cases {
		action, ok := keys.Match(tc.scope, tc.key)
		if !ok || action != tc.want {
			t.Errorf("Match(%q, %q) = %q, %v; want %q", tc.scope, tc.key, action, ok, tc.want)
		}
	}

	if _, ok := keys.Match(ScopePorts, "q"); ok {
		t.Error("the root quit key must not leak into the ports scope")
	}
	if _, ok := keys.Match(ScopeRoot, "x"); ok {
		t.Error("the kill key must not be global")
	}
}

func TestRegistryApply(t *testing.T) {
	t.Parallel()

	keys := Default()
	custom, err := keys.Apply(map[string][]string{
		"ports_kill": {"K"},
		"help":       {"h", "H"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got, want := custom.Label(ActionPortsKill), "K"; got != want {
		t.Errorf("Label(ports_kill) = %q, want %q", got, want)
	}
	if got, want := custom.Label(ActionHelp), "h / H"; got != want {
		t.Errorf("Label(help) = %q, want %q", got, want)
	}
	if got := keys.Label(ActionPortsKill); got != "x" {
		t.Errorf("Apply mutated the receiver: ports_kill = %q", got)
	}
}

func TestRegistryApplyRejects(t *testing.T) {
	t.Parallel()

	keys := Default()

	cases := []struct {
		name      string
		overrides map[string][]string
	}{
		{"unknown action", map[string][]string{"nope": {"z"}}},
		{"empty keys", map[string][]string{"help": {}}},
		{"invalid key", map[string][]string{"help": {"ctrl+ab"}}},
		{"reserved key", map[string][]string{"help": {"esc"}}},
		{"not configurable", map[string][]string{"filter_apply": {"z"}}},
	}
	for _, tc := range cases {
		if _, err := keys.Apply(tc.overrides); err == nil {
			t.Errorf("%s: Apply must fail", tc.name)
		}
	}
}

func TestRegistryApplyDeduplicates(t *testing.T) {
	t.Parallel()

	keys, err := Default().Apply(map[string][]string{"help": {"h", "h", "H"}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got, want := keys.Keys(ActionHelp), []string{"h", "H"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Keys(help) = %v, want %v", got, want)
	}
}

func TestRegistryKeysUsesFixedKeys(t *testing.T) {
	t.Parallel()

	keys := Default()
	if got, want := keys.Keys(ActionQuit), []string{"q", "ctrl+c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Keys(quit) = %v, want %v", got, want)
	}
	if got := keys.Keys("unknown"); got != nil {
		t.Errorf("Keys(unknown) = %v, want nil", got)
	}
}

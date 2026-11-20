package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeConfigFile creates the parent directories and writes body to path.
func writeConfigFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
}

func TestKeyListUnmarshalJSON(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{name: "single string", input: `"q"`, want: []string{"q"}},
		{name: "list", input: `["q","ctrl+c"]`, want: []string{"q", "ctrl+c"}},
		{name: "empty list", input: `[]`, want: []string{}},
		{name: "null", input: `null`, wantErr: true},
		{name: "number", input: `1`, wantErr: true},
		{name: "mixed list", input: `["q",1]`, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got KeyList

			err := json.Unmarshal([]byte(tc.input), &got)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Unmarshal(%s) error = nil, want an error", tc.input)
				}

				return
			}

			if err != nil {
				t.Fatalf("Unmarshal(%s) error = %v, want nil", tc.input, err)
			}
			if !slices.Equal([]string(got), tc.want) {
				t.Errorf("Unmarshal(%s) = %v, want %v", tc.input, []string(got), tc.want)
			}
		})
	}
}

func TestKeyListEmptyListPreserved(t *testing.T) {
	var got KeyList

	if err := json.Unmarshal([]byte(`[]`), &got); err != nil {
		t.Fatalf("Unmarshal([]) error = %v, want nil", err)
	}
	if got == nil {
		t.Error("Unmarshal([]) = nil, want an empty non-nil list")
	}
	if len(got) != 0 {
		t.Errorf("Unmarshal([]) = %v, want empty", got)
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name     string
		path     string
		explicit bool
		want     error
		wantLoad bool
	}{
		{name: "explicit missing", path: filepath.Join(dir, "nope.json"), explicit: true, want: ErrConfigFile},
		{name: "default missing", path: filepath.Join(dir, "absent.json"), explicit: false, wantLoad: false},
		{name: "empty path", path: "", explicit: false, wantLoad: false},
		{name: "directory", path: dir, explicit: true, want: ErrConfigFile},
		{name: "invalid json", path: filepath.Join(dir, "invalid.json"), explicit: true, want: ErrConfigFormat},
		{name: "unknown field", path: filepath.Join(dir, "unknown.json"), explicit: true, want: ErrConfigFormat},
		{name: "extra data", path: filepath.Join(dir, "extra.json"), explicit: true, want: ErrConfigFormat},
		{name: "null key value", path: filepath.Join(dir, "nullkey.json"), explicit: true, want: ErrConfigFormat},
		{name: "oversized", path: filepath.Join(dir, "big.json"), explicit: true, want: ErrConfigFile},
	}

	writeConfigFile(t, filepath.Join(dir, "invalid.json"), `{"theme":`)
	writeConfigFile(t, filepath.Join(dir, "unknown.json"), `{"nope":true}`)
	writeConfigFile(t, filepath.Join(dir, "extra.json"), `{"theme":"a"}{"theme":"b"}`)
	writeConfigFile(t, filepath.Join(dir, "nullkey.json"), `{"keys":{"quit":null}}`)
	writeConfigFile(t, filepath.Join(dir, "big.json"), strings.Repeat(" ", maxConfigBytes+1))

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, loaded, err := loadFile(tc.path, tc.explicit)
			if !errors.Is(err, tc.want) {
				t.Fatalf("loadFile() error = %v, want %v", err, tc.want)
			}
			if loaded != tc.wantLoad {
				t.Errorf("loadFile() loaded = %v, want %v", loaded, tc.wantLoad)
			}
		})
	}
}

func TestLoadFileValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeConfigFile(t, path, `{"theme":"tokyonight","split_width":44,"split_collapsed":true,"keys":{"quit":"q","ports_sort":["ctrl+s","o"]}}`)

	cfg, loaded, err := loadFile(path, true)
	if err != nil {
		t.Fatalf("loadFile() error = %v, want nil", err)
	}
	if !loaded {
		t.Error("loadFile() loaded = false, want true")
	}
	if cfg.Theme != "tokyonight" {
		t.Errorf("Theme = %q, want %q", cfg.Theme, "tokyonight")
	}
	if cfg.SplitWidth != 44 {
		t.Errorf("SplitWidth = %d, want 44", cfg.SplitWidth)
	}
	if !cfg.SplitCollapsed {
		t.Error("SplitCollapsed = false, want true")
	}
	if got := cfg.Keys["quit"]; !slices.Equal([]string(got), []string{"q"}) {
		t.Errorf("Keys[quit] = %v, want [q]", got)
	}
	if got := cfg.Keys["ports_sort"]; !slices.Equal([]string(got), []string{"ctrl+s", "o"}) {
		t.Errorf("Keys[ports_sort] = %v, want [ctrl+s o]", got)
	}
}

func TestDefaultConfigPath(t *testing.T) {
	dir := withDefaultConfigDir(t)

	want := filepath.Join(dir, "tui-port", "config.json")
	if got := defaultConfigPath(); got != want {
		t.Errorf("defaultConfigPath() = %q, want %q", got, want)
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MiguelFPP/tui-port/internal/config"
	"github.com/MiguelFPP/tui-port/internal/tui/styles"
)

// outputLines splits text into its non-empty lines, dropping the trailing newline.
func outputLines(text string) []string {
	trimmed := strings.TrimSuffix(text, "\n")
	if trimmed == "" {
		return nil
	}

	return strings.Split(trimmed, "\n")
}

func TestPrintThemes(t *testing.T) {
	var out bytes.Buffer

	printThemes(&out)

	lines := outputLines(out.String())
	if len(lines) != len(styles.Themes()) {
		t.Fatalf("printThemes() escribió %d líneas, want %d:\n%s", len(lines), len(styles.Themes()), out.String())
	}

	for _, want := range styles.ThemeNames() {
		if !strings.Contains(out.String(), want) {
			t.Errorf("printThemes() no incluye %q:\n%s", want, out.String())
		}
	}

	flagged := 0
	for _, line := range lines {
		if !strings.Contains(line, "(default)") {
			continue
		}

		flagged++
		if !strings.HasPrefix(line, styles.DefaultTheme().Name+" ") {
			t.Errorf("línea con (default) = %q", line)
		}
	}
	if flagged != 1 {
		t.Errorf("printThemes() marcó %d temas por defecto, want 1", flagged)
	}
}

func TestPrintKeys(t *testing.T) {
	var out bytes.Buffer

	printKeys(&out)

	text := out.String()
	lines := outputLines(text)
	if lines[0] != "accion\tscope\tteclas\ttipo" {
		t.Errorf("printKeys() cabecera = %q", lines[0])
	}

	for _, want := range []string{
		"quit\troot\tq / ctrl+c\tconfigurable",
		"ports_kill\tports\tx\tconfigurable",
		"filter_apply\tfilter\tenter\tfija",
	} {
		if !strings.Contains("\n"+text, "\n"+want+"\n") {
			t.Errorf("printKeys() no incluye %q:\n%s", want, text)
		}
	}
}

func TestPrintConfig(t *testing.T) {
	var out bytes.Buffer

	cfg := config.Config{
		ConfigPath:     "/tmp/config.json",
		Theme:          "dracula",
		SplitWidth:     44,
		SplitCollapsed: true,
		Keys:           map[string][]string{"help": {"h"}},
	}
	if err := printConfig(&out, cfg); err != nil {
		t.Fatalf("printConfig: %v", err)
	}

	var decoded struct {
		ConfigPath     string              `json:"config_path"`
		Theme          string              `json:"theme"`
		SplitWidth     int                 `json:"split_width"`
		SplitCollapsed bool                `json:"split_collapsed"`
		Keys           map[string][]string `json:"keys"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("printConfig wrote invalid JSON: %v\n%s", err, out.String())
	}

	if decoded.ConfigPath != "/tmp/config.json" || decoded.Theme != "dracula" || decoded.SplitWidth != 44 || !decoded.SplitCollapsed {
		t.Errorf("printConfig() = %+v", decoded)
	}
	if got := decoded.Keys["help"]; len(got) != 1 || got[0] != "h" {
		t.Errorf("keys = %v", decoded.Keys)
	}
}

func TestPrintConfigDefaults(t *testing.T) {
	var out bytes.Buffer
	if err := printConfig(&out, config.Config{}); err != nil {
		t.Fatalf("printConfig: %v", err)
	}

	if !strings.Contains(out.String(), `"theme": "`+styles.DefaultTheme().Name+`"`) {
		t.Errorf("printConfig() must resolve the default theme:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `"keys": {}`) {
		t.Errorf("printConfig() must emit an empty object, not null:\n%s", out.String())
	}
}

func TestPrintConfigRejectsUnknownTheme(t *testing.T) {
	var out bytes.Buffer
	if err := printConfig(&out, config.Config{Theme: "nope"}); err == nil {
		t.Error("printConfig must reject an unknown theme")
	}
}

func TestVersionLine(t *testing.T) {
	original := commit
	t.Cleanup(func() { commit = original })

	commit = "dev"
	if got, want := versionLine(), "tui-port "+version; got != want {
		t.Errorf("versionLine() = %q, want %q", got, want)
	}

	commit = "abc123"
	if got := versionLine(); !strings.Contains(got, "abc123") {
		t.Errorf("versionLine() = %q, want the injected commit", got)
	}
}

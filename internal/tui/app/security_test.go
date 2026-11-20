package app

import (
	"strings"
	"testing"

	"github.com/MiguelFPP/tui-port/internal/config"
)

func TestConfigOverlayNeutralizesThePath(t *testing.T) {
	deps := testDeps()
	deps.Config = config.Config{
		ConfigPath: "/tmp/\x1b]0;owned\x07/config.json",
		Keys:       map[string][]string{"help": {"h"}},
	}

	m := New(deps)
	m = update(t, m, typed(','))

	content := m.View().Content
	for _, sequence := range []string{"\x1b]0", "\x07"} {
		if strings.Contains(content, sequence) {
			t.Fatalf("the config overlay leaked %q: %q", sequence, content)
		}
	}
	if !strings.Contains(content, "config.json") {
		t.Fatalf("the config overlay lost the path: %q", content)
	}
}

func TestToastNeutralizesTerminalSequences(t *testing.T) {
	m := New(testDeps())
	m.notify("\x1b]0;owned\x07")

	content := m.View().Content
	for _, sequence := range []string{"\x1b]0", "\x07"} {
		if strings.Contains(content, sequence) {
			t.Fatalf("the toast leaked %q: %q", sequence, content)
		}
	}
}

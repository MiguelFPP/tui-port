package inspector

import (
	"strings"
	"testing"

	"github.com/MiguelFPP/tui-port/internal/tui/keymap"
)

func TestViewNeutralizesProcessMetadata(t *testing.T) {
	process := testProcess()
	process.Name = "\x1b]52;c;SGVtbw==\x07node"
	process.User = "oda\x1b[2J"
	process.Cmdline = "node\x1b]0;owned\x07server.js"

	m := New(keymap.Default())
	m.SetBounds(0, 0, testWidth, testHeight)
	m.SetSelection(testSocket(), process, testSiblings(), true)

	view := m.View()
	for _, sequence := range []string{"\x1b]52", "\x1b[2J", "\x1b]0", "\x07"} {
		if strings.Contains(view, sequence) {
			t.Fatalf("View() leaked %q: %q", sequence, view)
		}
	}
	if !strings.Contains(view, "node") {
		t.Fatalf("View() = %q, want the printable process name", view)
	}
}

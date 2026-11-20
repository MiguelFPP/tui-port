package inspector

import (
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/MiguelFPP/tui-port/internal/domain"
	"github.com/MiguelFPP/tui-port/internal/tui/keymap"
)

const (
	testWidth  = 80
	testHeight = 24

	// testRSS is 120.4 MiB, the value the attributed fixture renders.
	testRSS = uint64(126_248_550)
)

// startedPattern matches the start row: the timestamp plus the human uptime.
var startedPattern = regexp.MustCompile(`inicio: \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \(\d+[dhm]`)

// testSocket is the selected endpoint: a TCP listener owned by testProcess.
func testSocket() domain.Socket {
	return domain.Socket{
		Protocol: domain.ProtocolTCP,
		Local:    domain.Endpoint{Addr: "127.0.0.1", Port: 3000},
		State:    domain.StateListen,
		UID:      1000,
		Inode:    123456,
		PID:      4242,
	}
}

// testProcess is the owner of the selected socket.
func testProcess() domain.Process {
	return domain.Process{
		PID:       4242,
		PPID:      1000,
		Name:      "node",
		Cmdline:   "node server.js --port 3000 --verbose --watch",
		User:      "oda",
		RSS:       testRSS,
		Threads:   12,
		StartedAt: time.Now().Add(-2*time.Hour - 3*time.Minute),
	}
}

// testSiblings are the ports of the process; the first one is the selection.
func testSiblings() []domain.Socket {
	return []domain.Socket{
		{Protocol: domain.ProtocolTCP, Local: domain.Endpoint{Addr: "0.0.0.0", Port: 3000}, State: domain.StateListen, Inode: 123456, PID: 4242},
		{Protocol: domain.ProtocolUDP, Local: domain.Endpoint{Addr: "0.0.0.0", Port: 5353}, State: domain.StateListen, Inode: 123457, PID: 4242},
		{Protocol: domain.ProtocolTCP, Local: domain.Endpoint{Addr: "127.0.0.1", Port: 8080}, State: domain.StateEstablished, Inode: 123458, PID: 4242},
	}
}

// longSiblings fill the pane past its viewport, so the scroll can be exercised.
func longSiblings() []domain.Socket {
	siblings := make([]domain.Socket, 0, 12)
	siblings = append(siblings, testSiblings()...)

	for port := 3001; port <= 3009; port++ {
		siblings = append(siblings, domain.Socket{
			Protocol: domain.ProtocolTCP,
			Local:    domain.Endpoint{Addr: "127.0.0.1", Port: uint16(port)},
			State:    domain.StateListen,
			Inode:    uint64(123500 + port),
			PID:      4242,
		})
	}

	return siblings
}

// newSelected builds a pane with the attributed fixture already selected.
func newSelected(t *testing.T, width, height int) Model {
	t.Helper()

	m := New(keymap.Default())
	m.SetBounds(0, 0, width, height)
	m.SetSelection(testSocket(), testProcess(), testSiblings(), true)

	return m
}

// newLong builds a pane whose content is taller than any short viewport.
func newLong(t *testing.T, keys keymap.Registry) Model {
	t.Helper()

	m := New(keys)
	m.SetBounds(0, 0, 40, 10)
	m.SetSelection(testSocket(), testProcess(), longSiblings(), true)

	return m
}

// press sends one key press and returns the updated model.
func press(t *testing.T, m Model, msg tea.KeyPressMsg) Model {
	t.Helper()

	next, cmd := m.Update(msg)
	if cmd != nil {
		t.Fatalf("key %q returned a command", msg.String())
	}

	return next
}

// assertLayout checks the invariants of every frame the root draws.
func assertLayout(t *testing.T, view string, width, height int) {
	t.Helper()

	lines := strings.Split(view, "\n")
	if len(lines) != height {
		t.Fatalf("view has %d lines, want %d:\n%s", len(lines), height, view)
	}

	for index, line := range lines {
		if got := lipgloss.Width(line); got != width {
			t.Fatalf("line %d has %d cells, want %d: %q", index, got, width, line)
		}
	}
}

func TestViewAttributed(t *testing.T) {
	m := newSelected(t, testWidth, testHeight)
	view := m.View()

	want := []string{
		"detalle · socket",
		"proto: tcp",
		"local: 127.0.0.1:3000",
		"remoto: -",
		"estado: listen",
		"uid: 1000 · inode: 123456",
		"nombre: node (pid 4242)",
		"usuario: oda · ppid: 1000",
		"rss: 120.4 MiB · hilos: 12",
		"cmdline: node server.js --port 3000 --verbose --watch",
		"puertos del proceso (3)",
		"▸ tcp *:3000 listen",
		"udp *:5353 listen",
		"tcp 127.0.0.1:8080 established",
		"100%",
	}
	for _, fragment := range want {
		if !strings.Contains(view, fragment) {
			t.Fatalf("view does not contain %q:\n%s", fragment, view)
		}
	}

	// The start row carries the timestamp and the human uptime of the process.
	if !startedPattern.MatchString(view) {
		t.Fatalf("view does not render the start of the process:\n%s", view)
	}

	if got := strings.Count(view, "▸"); got != 1 {
		t.Fatalf("view marks %d ports as selected, want 1:\n%s", got, view)
	}

	// The ports of the process are sorted by local port.
	first := strings.Index(view, "*:3000")
	second := strings.Index(view, "*:5353")
	third := strings.Index(view, "127.0.0.1:8080")
	if first < 0 || second < 0 || third < 0 || !(first < second && second < third) {
		t.Fatalf("ports are not sorted by local port:\n%s", view)
	}

	assertLayout(t, view, testWidth, testHeight)
}

func TestViewLayout(t *testing.T) {
	cases := []struct {
		name   string
		width  int
		height int
	}{
		{name: "narrow", width: 24, height: 10},
		{name: "half", width: 40, height: 10},
		{name: "wide", width: 80, height: 24},
		{name: "wide-short", width: 40, height: 24},
		{name: "collapsed", width: 12, height: 6},
		{name: "no-width", width: 0, height: 5},
		{name: "one-row", width: 40, height: 1},
		{name: "two-rows", width: 40, height: 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newSelected(t, tc.width, tc.height)
			assertLayout(t, m.View(), tc.width, tc.height)
		})
	}
}

func TestViewUnattributed(t *testing.T) {
	m := New(keymap.Default())
	m.SetBounds(0, 0, 60, 24)

	socket := testSocket()
	socket.PID = 0
	m.SetSelection(socket, domain.Process{}, nil, true)

	view := m.View()
	for _, fragment := range []string{
		"sin proceso identificable",
		"uid 1000 · ejecuta como root para ver todos los procesos",
	} {
		if !strings.Contains(view, fragment) {
			t.Fatalf("view does not contain %q:\n%s", fragment, view)
		}
	}

	if strings.Contains(view, "puertos del proceso") {
		t.Fatalf("an unattributed socket must not list ports:\n%s", view)
	}

	assertLayout(t, view, 60, 24)
}

func TestViewEmpty(t *testing.T) {
	m := New(keymap.Default())
	m.SetSelection(domain.Socket{}, domain.Process{}, nil, false)

	cases := []struct {
		name   string
		width  int
		height int
	}{
		{name: "narrow", width: 24, height: 10},
		{name: "wide", width: 80, height: 24},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m.SetBounds(0, 0, tc.width, tc.height)

			view := m.View()
			if !strings.Contains(view, "selecciona un puerto") {
				t.Fatalf("view does not show the placeholder:\n%s", view)
			}
			if strings.Contains(view, "sin proceso identificable") {
				t.Fatalf("an empty selection must not render the process section:\n%s", view)
			}

			assertLayout(t, view, tc.width, tc.height)
		})
	}
}

func TestSetSelectionKeepsAndResetsScroll(t *testing.T) {
	socket, process, siblings := testSocket(), testProcess(), longSiblings()

	m := New(keymap.Default())
	m.SetBounds(0, 0, 40, 10)
	m.SetSelection(socket, process, siblings, true)

	for range 5 {
		m = press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	}

	offset := m.viewport.YOffset()
	if offset == 0 {
		t.Fatalf("the long content did not scroll")
	}

	lines := m.lines
	m.SetSelection(socket, process, siblings, true)
	if got := m.viewport.YOffset(); got != offset {
		t.Fatalf("reselecting the same socket moved the scroll to %d, want %d", got, offset)
	}
	if len(lines) == 0 || len(m.lines) == 0 || &lines[0] != &m.lines[0] {
		t.Fatalf("reselecting the same socket rebuilt the rendered lines")
	}

	// New data for the same socket keeps the scroll: only the socket identity resets it.
	changed := process
	changed.RSS = 2048 * 1024
	m.SetSelection(socket, changed, siblings, true)
	if got := m.viewport.YOffset(); got != offset {
		t.Fatalf("a data change of the same socket moved the scroll to %d, want %d", got, offset)
	}

	// A different socket starts at the top again.
	other := socket
	other.Inode = 999999
	other.Local.Port = 4000
	m.SetSelection(other, process, siblings, true)
	if got := m.viewport.YOffset(); got != 0 {
		t.Fatalf("selecting another socket left the scroll at %d, want 0", got)
	}
}

func TestUpdateScrollsTheViewport(t *testing.T) {
	m := newLong(t, keymap.Default())

	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.viewport.YOffset(); got != 1 {
		t.Fatalf("down left the offset at %d, want 1", got)
	}

	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if got := m.viewport.YOffset(); got <= 1 {
		t.Fatalf("pgdown left the offset at %d, want more than 1", got)
	}

	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnd})
	if !m.viewport.AtBottom() {
		t.Fatalf("end did not reach the bottom")
	}

	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyHome})
	if got := m.viewport.YOffset(); got != 0 {
		t.Fatalf("home left the offset at %d, want 0", got)
	}

	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if got := m.viewport.YOffset(); got != 0 {
		t.Fatalf("pgup at the top left the offset at %d, want 0", got)
	}

	// Keys outside the inspector scope are ignored.
	before := m.viewport.YOffset()
	m = press(t, m, tea.KeyPressMsg{Code: 'z', Text: "z"})
	if got := m.viewport.YOffset(); got != before {
		t.Fatalf("an unrelated key moved the offset from %d to %d", before, got)
	}
}

func TestMouseWheelScrolls(t *testing.T) {
	m := newLong(t, keymap.Default())

	next, cmd := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if cmd != nil {
		t.Fatalf("the wheel returned a command")
	}
	if got := next.viewport.YOffset(); got == 0 {
		t.Fatalf("the wheel did not scroll the viewport")
	}
}

func TestWindowSizeIsIgnored(t *testing.T) {
	m := newSelected(t, testWidth, testHeight)

	next, _ := m.Update(tea.WindowSizeMsg{Width: 10, Height: 5})
	if next.width != testWidth || next.height != testHeight {
		t.Fatalf("the window size changed the bounds to %dx%d", next.width, next.height)
	}

	assertLayout(t, next.View(), testWidth, testHeight)
}

func TestNewFallsBackToDefaults(t *testing.T) {
	m := newLong(t, keymap.Registry{})

	next := press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := next.viewport.YOffset(); got != 1 {
		t.Fatalf("the default keymap left the offset at %d, want 1", got)
	}
}

func TestCustomBindingScrolls(t *testing.T) {
	keys, err := keymap.Default().Apply(map[string][]string{
		string(keymap.ActionInspectorDown): {"n"},
	})
	if err != nil {
		t.Fatalf("Apply returned an error: %v", err)
	}

	m := newLong(t, keys)

	next := press(t, m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if got := next.viewport.YOffset(); got != 1 {
		t.Fatalf("the overridden key left the offset at %d, want 1", got)
	}
}

func TestCmdlineWrapping(t *testing.T) {
	process := testProcess()
	process.Cmdline = "node\t/usr/local/bin/server.js --port 3000 --watch --verbose --cluster 4 " +
		"--inspect=127.0.0.1:9229 --max-old-space-size=4096 --trace-warnings --enable-source-maps " +
		"--experimental-loader ./loader.mjs --experimental-vm-modules\x00\x1b[31m"

	m := New(keymap.Default())
	m.SetBounds(0, 0, 40, 24)
	m.SetSelection(testSocket(), process, testSiblings(), true)

	if len(m.lines) == 0 {
		t.Fatalf("the pane rendered no lines")
	}

	pad := strings.Repeat(" ", len(cmdlineLabel))
	rows := make([]string, 0, maxCmdlineLines)

	for index, line := range m.lines {
		if got := lipgloss.Width(line); got > 40 {
			t.Fatalf("content line %d is %d cells wide, want at most 40: %q", index, got, line)
		}

		if strings.HasPrefix(line, cmdlineLabel) || strings.HasPrefix(line, pad) {
			rows = append(rows, line)
		}
	}

	if len(rows) != maxCmdlineLines {
		t.Fatalf("the command line rendered %d rows, want %d: %q", len(rows), maxCmdlineLines, rows)
	}
	if !strings.HasPrefix(rows[0], cmdlineLabel) {
		t.Fatalf("the first command line row does not carry the label: %q", rows[0])
	}

	for _, row := range rows {
		for _, r := range row {
			if unicode.IsControl(r) {
				t.Fatalf("a command line row keeps the control rune %q: %q", r, row)
			}
		}
	}

	for _, row := range rows[1:] {
		if !strings.HasPrefix(row, pad) {
			t.Fatalf("a continuation row is not aligned under the label: %q", row)
		}
	}

	if last := rows[len(rows)-1]; !strings.HasSuffix(last, "…") {
		t.Fatalf("the truncated command line does not end with an ellipsis: %q", last)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		size uint64
		want string
	}{
		{size: 0, want: "0 B"},
		{size: 999, want: "999 B"},
		{size: 1024, want: "1.0 KiB"},
		{size: 1536, want: "1.5 KiB"},
		{size: testRSS, want: "120.4 MiB"},
		{size: 2 * 1024 * 1024 * 1024, want: "2.0 GiB"},
	}

	for _, tc := range cases {
		if got := humanBytes(tc.size); got != tc.want {
			t.Fatalf("humanBytes(%d) = %q, want %q", tc.size, got, tc.want)
		}
	}
}

func TestUptimeLabel(t *testing.T) {
	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		started time.Time
		want    string
	}{
		{name: "days", started: now.Add(-(2*24*time.Hour + 3*time.Hour)), want: "2d 3h"},
		{name: "hours", started: now.Add(-(5*time.Hour + 7*time.Minute)), want: "5h 7m"},
		{name: "minutes", started: now.Add(-12 * time.Minute), want: "12m"},
		{name: "future", started: now.Add(time.Hour), want: "0m"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := uptimeLabel(tc.started, now); got != tc.want {
				t.Fatalf("uptimeLabel() = %q, want %q", got, tc.want)
			}
		})
	}

	if got := startedLabel(time.Time{}, now); got != "-" {
		t.Fatalf("a zero start rendered %q, want %q", got, "-")
	}
}

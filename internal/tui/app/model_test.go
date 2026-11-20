package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/MiguelFPP/tui-port/internal/config"
	"github.com/MiguelFPP/tui-port/internal/domain"
	"github.com/MiguelFPP/tui-port/internal/tui/keymap"
)

func testDeps() Deps {
	return Deps{
		Config: config.Config{},
		Keys:   keymap.Default(),
		SaveSplit: func(int, bool) error {
			return nil
		},
	}
}

func typed(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func key(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func update(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()

	next, _ := m.Update(msg)
	model, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want app.Model", next)
	}

	return model
}

// drain runs one command tree, feeding its messages back like the runtime does.
func drain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()

	if cmd == nil {
		return m
	}

	return applyMsg(t, m, cmd())
}

func applyMsg(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()

	if msg == nil {
		return m
	}

	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, nested := range batch {
			if nested == nil {
				continue
			}

			m = applyMsg(t, m, nested())
		}

		return m
	}

	return update(t, m, msg)
}

func press(t *testing.T, m Model, msg tea.KeyPressMsg) (Model, tea.Cmd) {
	t.Helper()

	return send(t, m, msg)
}

func send(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()

	next, cmd := m.Update(msg)
	model, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want app.Model", next)
	}

	return model, cmd
}

func TestNewDefaults(t *testing.T) {
	t.Parallel()

	m := New(testDeps())

	if m.Tab() != ListenTab {
		t.Errorf("Tab() = %v, want ListenTab", m.Tab())
	}
	if m.Focus() != FocusTable {
		t.Errorf("Focus() = %v, want FocusTable", m.Focus())
	}
	if m.table.Scope() != domain.ScopeListen {
		t.Errorf("table scope = %v, want listen", m.table.Scope())
	}
	if m.InspectorCollapsed() {
		t.Error("the inspector must start visible")
	}
	if m.HelpVisible() || m.ConfigVisible() {
		t.Error("no overlay must start open")
	}
}

func TestConfiguredLayout(t *testing.T) {
	t.Parallel()

	deps := testDeps()
	deps.Config = config.Config{SplitWidth: 50, SplitCollapsed: true}
	m := New(deps)

	if m.SplitWidth() != 50 || !m.InspectorCollapsed() {
		t.Errorf("layout = %d collapsed=%v", m.SplitWidth(), m.InspectorCollapsed())
	}
	if m.Focus() != FocusTable {
		t.Errorf("Focus() = %v, want the table when the inspector is collapsed", m.Focus())
	}
}

func TestTabsSwitchScope(t *testing.T) {
	t.Parallel()

	m := New(testDeps())

	m = update(t, m, typed('2'))
	if m.Tab() != ConnectedTab || m.table.Scope() != domain.ScopeConnected {
		t.Errorf("tab=%v scope=%v, want connected", m.Tab(), m.table.Scope())
	}

	m = update(t, m, typed('3'))
	if m.Tab() != AllTab || m.table.Scope() != domain.ScopeAll {
		t.Errorf("tab=%v scope=%v, want all", m.Tab(), m.table.Scope())
	}

	m = update(t, m, typed('1'))
	if m.Tab() != ListenTab || m.table.Scope() != domain.ScopeListen {
		t.Errorf("tab=%v scope=%v, want listen", m.Tab(), m.table.Scope())
	}
}

func TestTabClick(t *testing.T) {
	t.Parallel()

	m := New(testDeps())
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	// The tab row sits at y=1: " Listen " | " Conectadas " | " Todas ".
	m = update(t, m, click(10, 1))
	if m.Tab() != ConnectedTab {
		t.Errorf("Tab() = %v after clicking the second label", m.Tab())
	}

	m = update(t, m, click(1, 1))
	if m.Tab() != ListenTab {
		t.Errorf("Tab() = %v after clicking the first label", m.Tab())
	}
}

func TestFocusToggles(t *testing.T) {
	t.Parallel()

	m := New(testDeps())

	m = update(t, m, key(tea.KeyTab))
	if m.Focus() != FocusInspector {
		t.Fatalf("Focus() = %v, want the inspector", m.Focus())
	}

	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.Focus() != FocusTable {
		t.Errorf("Focus() = %v, want the table", m.Focus())
	}

	m = update(t, m, key(tea.KeyF2))
	if !m.InspectorCollapsed() || m.Focus() != FocusTable {
		t.Fatalf("collapsed=%v focus=%v after f2", m.InspectorCollapsed(), m.Focus())
	}

	m = update(t, m, key(tea.KeyTab))
	if m.Focus() != FocusTable {
		t.Error("a collapsed inspector must never take the keyboard")
	}
}

func TestToggleInspectorPersists(t *testing.T) {
	t.Parallel()

	var savedWidth int
	var savedCollapsed bool
	calls := 0

	deps := testDeps()
	deps.SaveSplit = func(width int, collapsed bool) error {
		calls++
		savedWidth, savedCollapsed = width, collapsed

		return nil
	}
	m := New(deps)

	_, cmd := press(t, m, key(tea.KeyF2))
	drain(t, m, cmd)

	if calls != 1 || !savedCollapsed {
		t.Errorf("SaveSplit calls=%d collapsed=%v", calls, savedCollapsed)
	}
	if savedWidth != 0 {
		t.Errorf("saved width = %d, want the automatic 0", savedWidth)
	}
}

func TestSplitResizePersists(t *testing.T) {
	t.Parallel()

	var savedWidth int
	deps := testDeps()
	deps.SaveSplit = func(width int, _ bool) error {
		savedWidth = width

		return nil
	}
	m := New(deps)
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	before := m.tableWidthNow(100)
	next, cmd := press(t, m, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift})
	next = drain(t, next, cmd)

	if got := next.tableWidthNow(100); got != before+splitStep {
		t.Errorf("table width = %d, want %d", got, before+splitStep)
	}
	if savedWidth != before+splitStep {
		t.Errorf("saved width = %d, want %d", savedWidth, before+splitStep)
	}
}

func TestCollapseButton(t *testing.T) {
	t.Parallel()

	m := New(testDeps())
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	// The button is right-aligned on the tab row: "[>]" at columns 77..80.
	m = update(t, m, click(78, 1))
	if !m.InspectorCollapsed() {
		t.Fatal("clicking the button must collapse the inspector")
	}

	m = update(t, m, click(78, 1))
	if m.InspectorCollapsed() {
		t.Error("clicking the button again must show the inspector")
	}
}

func TestSplitterDrag(t *testing.T) {
	t.Parallel()

	var savedWidth int
	deps := testDeps()
	deps.SaveSplit = func(width int, _ bool) error {
		savedWidth = width

		return nil
	}
	m := New(deps)
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	tableW := m.tableWidthNow(100)
	row := headerRows + 3

	m = update(t, m, click(tableW-1, row))
	if !m.dragging {
		t.Fatal("clicking the shared border must start a drag")
	}

	m = update(t, m, tea.MouseMotionMsg{X: tableW + 9, Y: row})
	next, cmd := press(t, m, typed('z')) // an unbound key must not end the drag
	next = drain(t, next, cmd)
	if !next.dragging {
		t.Fatal("a key press must not end the drag")
	}

	next, cmd = send(t, next, tea.MouseReleaseMsg{X: tableW + 9, Y: row})
	next = drain(t, next, cmd)
	if next.dragging {
		t.Error("release must end the drag")
	}
	if savedWidth != next.tableWidthNow(100) {
		t.Errorf("saved width = %d, want %d", savedWidth, next.tableWidthNow(100))
	}
}

func TestHelpOverlay(t *testing.T) {
	t.Parallel()

	m := New(testDeps())
	m = update(t, m, typed('?'))
	if !m.HelpVisible() {
		t.Fatal("? must open the help overlay")
	}
	if !strings.Contains(m.View().Content, "tui-port · atajos") {
		t.Error("the help overlay must render its title")
	}

	m = update(t, m, typed('?'))
	if m.HelpVisible() {
		t.Error("? must close the help overlay")
	}

	m = update(t, m, typed('?'))
	m = update(t, m, key(tea.KeyEscape))
	if m.HelpVisible() {
		t.Error("esc must close the help overlay")
	}
}

func TestConfigOverlay(t *testing.T) {
	t.Parallel()

	deps := testDeps()
	deps.Config = config.Config{Theme: "dracula", Keys: map[string][]string{"help": {"h"}}}
	m := New(deps)

	m = update(t, m, typed(','))
	if !m.ConfigVisible() {
		t.Fatal(", must open the configuration overlay")
	}
	content := m.View().Content
	if !strings.Contains(content, "configuración") || !strings.Contains(content, "help: h") {
		t.Errorf("the configuration overlay lacks its content:\n%s", content)
	}

	m = update(t, m, key(tea.KeyEscape))
	if m.ConfigVisible() {
		t.Error("esc must close the configuration overlay")
	}
}

func TestQuitKeys(t *testing.T) {
	t.Parallel()

	m := New(testDeps())

	for _, msg := range []tea.KeyPressMsg{typed('q'), {Code: 'c', Mod: tea.ModCtrl}} {
		next, cmd := press(t, m, msg)
		if cmd == nil {
			t.Fatalf("%q must return a quit command", msg.String())
		}

		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%q command = %T, want tea.QuitMsg", msg.String(), cmd())
		}
		_ = next
	}
}

func TestFilterCapturesInput(t *testing.T) {
	t.Parallel()

	m := New(testDeps())
	m = update(t, m, typed('/'))
	if !m.table.CapturesInput() {
		t.Fatal("/ must open the filter input")
	}

	// q is typed into the filter instead of quitting.
	next, cmd := press(t, m, typed('q'))
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("q must not quit while the filter is being edited")
		}
	}
	if !next.table.CapturesInput() {
		t.Error("the filter must keep capturing input")
	}

	next = update(t, next, key(tea.KeyEscape))
	if next.table.CapturesInput() {
		t.Error("esc must close the filter")
	}
}

func TestRenderShape(t *testing.T) {
	t.Parallel()

	m := New(testDeps())
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	content := m.View().Content
	lines := strings.Split(content, "\n")
	if len(lines) != 30 {
		t.Fatalf("render = %d lines, want 30", len(lines))
	}

	for i, line := range lines {
		if width := lipgloss.Width(line); width > 100 {
			t.Errorf("line %d is %d cells wide, want <= 100", i, width)
		}
	}

	if !strings.Contains(content, "puertos: 0") {
		t.Error("the status bar must show the totals")
	}
}

func TestToastLifecycle(t *testing.T) {
	t.Parallel()

	m := New(testDeps())
	m.notify("hola")

	if !strings.Contains(m.View().Content, "aviso: hola") {
		t.Error("the toast must render in the status bar")
	}

	m = update(t, m, toastExpiredMsg{seq: m.toastSeq})
	if strings.Contains(m.View().Content, "aviso: hola") {
		t.Error("an expired toast must be cleared")
	}
}

func TestCollapsedLayoutUsesFullWidth(t *testing.T) {
	t.Parallel()

	deps := testDeps()
	deps.Config = config.Config{SplitCollapsed: true}
	m := New(deps)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	if got, want := m.tableWidthNow(80), 80; got != want {
		t.Errorf("table width = %d, want %d", got, want)
	}
	if !m.inspectorRect.Empty() {
		t.Error("a collapsed inspector must leave no clickable rectangle")
	}
}

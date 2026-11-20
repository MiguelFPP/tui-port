// Package app holds the root Bubble Tea model of the TUI.
package app

import (
	"image"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-port/internal/config"
	"github.com/MiguelFPP/tui-port/internal/domain"
	"github.com/MiguelFPP/tui-port/internal/ports"
	"github.com/MiguelFPP/tui-port/internal/tui/components/inspector"
	"github.com/MiguelFPP/tui-port/internal/tui/components/porttable"
	"github.com/MiguelFPP/tui-port/internal/tui/keymap"
	"github.com/MiguelFPP/tui-port/internal/tui/styles"
)

const (
	defaultWidth  = 80
	defaultHeight = 24

	minWidth  = 40
	minHeight = 10

	headerRows = 2
	footerRows = 1

	minTable      = 40
	tablePercent  = 62
	minInspector  = 24
	splitStep     = 2
	toastTTL      = 3 * time.Second
	toastPrefix   = "aviso: "
	tabSeparator  = "│"
	resetStyle    = "\x1b[m"
	borderSide    = 1
	helpPadding   = 2
	overlayChrome = 2 * (borderSide + helpPadding)
)

// Tab is one top-level section of the TUI; each one filters the same table.
type Tab uint8

const (
	ListenTab Tab = iota
	ConnectedTab
	AllTab

	tabCount = 3
)

// tabLabels are the rendered labels, shared by the tab row and its hit-testing.
var tabLabels = [tabCount]string{ListenTab: " Listen ", ConnectedTab: " Conectadas ", AllTab: " Todas "}

func (t Tab) String() string {
	if t >= tabCount {
		return strings.TrimSpace(tabLabels[ListenTab])
	}

	return strings.TrimSpace(tabLabels[t])
}

// Scope is the filter the tab applies to the socket table.
func (t Tab) Scope() domain.Scope {
	switch t {
	case ConnectedTab:
		return domain.ScopeConnected
	case AllTab:
		return domain.ScopeAll
	default:
		return domain.ScopeListen
	}
}

// Focus is the panel that receives the keyboard.
type Focus uint8

const (
	FocusTable Focus = iota
	FocusInspector
)

// Deps are the collaborators injected by the composition root. Keys carries the
// user keymap; an empty registry falls back to the built-in defaults.
type Deps struct {
	Config  config.Config
	Monitor ports.MonitorService
	Keys    keymap.Registry

	// SaveSplit persists the table width and the collapsed inspector; nil means
	// the layout is not persisted.
	SaveSplit func(width int, collapsed bool) error
}

// Model is the root model: it owns the tabs, the layout, the focus and the toasts.
type Model struct {
	cfg    config.Config
	keys   keymap.Registry
	table  porttable.Model
	detail inspector.Model

	tab   Tab
	focus Focus

	width  int
	height int

	tabRects      [tabCount]image.Rectangle
	tableRect     image.Rectangle
	inspectorRect image.Rectangle
	splitterRect  image.Rectangle
	collapseRect  image.Rectangle

	splitWidth         int
	inspectorCollapsed bool
	dragging           bool
	dragDirty          bool
	saveSplit          func(int, bool) error

	help       bool
	configOpen bool

	spinner  spinner.Model
	spinning bool

	toast    string
	toastSeq int
	lastErr  error
}

// toastExpiredMsg clears the toast unless a newer one has taken its place.
type toastExpiredMsg struct {
	seq int
}

// layoutSavedMsg carries the result of persisting the split layout.
type layoutSavedMsg struct {
	err error
}

// New builds the root model for the given dependencies.
func New(deps Deps) Model {
	keys := deps.Keys
	if len(keys.All()) == 0 {
		keys = keymap.Default()
	}

	m := Model{
		cfg:                deps.Config,
		keys:               keys,
		table:              porttable.New(porttable.Deps{Monitor: deps.Monitor, Keys: keys}),
		detail:             inspector.New(keys),
		tab:                ListenTab,
		focus:              FocusTable,
		width:              defaultWidth,
		height:             defaultHeight,
		splitWidth:         deps.Config.SplitWidth,
		inspectorCollapsed: deps.Config.SplitCollapsed,
		saveSplit:          deps.SaveSplit,
		spinner:            spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(styles.Spinner)),
		spinning:           true,
	}
	m.table.SetScope(m.tab.Scope())
	m.resize()

	return m
}

// Init starts the socket poller and the spinner.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.table.Init(), m.spinner.Tick)
}

// Update routes one message: keys and mouse to the focused panel, the rest by fan-out.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, 4)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
	case tea.KeyPressMsg:
		cmd, stop := m.handleKey(msg)
		if stop {
			return m, cmd
		}

		cmds = append(cmds, cmd)
	case tea.MouseClickMsg:
		cmds = append(cmds, m.handleClick(msg))
	case tea.MouseMotionMsg:
		cmds = append(cmds, m.handleMotion(msg))
	case tea.MouseReleaseMsg:
		cmds = append(cmds, m.handleRelease(msg))
	case tea.MouseWheelMsg:
		cmds = append(cmds, m.handleWheel(msg))
	case spinner.TickMsg:
		var tick tea.Cmd
		m.spinner, tick = m.spinner.Update(msg)
		m.spinning = false
		if m.loading() {
			m.spinning = true
			cmds = append(cmds, tick)
		}
	case toastExpiredMsg:
		if msg.seq == m.toastSeq {
			m.toast = ""
		}
	case layoutSavedMsg:
		if msg.err != nil {
			cmds = append(cmds, m.notify(msg.err.Error()))
		}
	default:
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		cmds = append(cmds, cmd)
	}

	m.syncSelection()
	cmds = append(cmds, m.afterUpdate())

	return m, tea.Batch(cmds...)
}

// View renders the layout; the alternate screen and the mouse are set here.
func (m Model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion

	return view
}

// Tab returns the active tab.
func (m Model) Tab() Tab {
	return m.tab
}

// Focus returns the panel that owns the keyboard.
func (m Model) Focus() Focus {
	return m.focus
}

// HelpVisible reports whether the help overlay is open.
func (m Model) HelpVisible() bool {
	return m.help
}

// ConfigVisible reports whether the read-only configuration overlay is open.
func (m Model) ConfigVisible() bool {
	return m.configOpen
}

// InspectorCollapsed reports whether the detail pane is hidden.
func (m Model) InspectorCollapsed() bool {
	return m.inspectorCollapsed
}

// SplitWidth returns the explicit table width; zero means automatic.
func (m Model) SplitWidth() int {
	return m.splitWidth
}

// syncSelection feeds the detail pane with the selected socket of the table.
func (m *Model) syncSelection() {
	socket, process, siblings, ok := m.table.Selection()
	m.detail.SetSelection(socket, process, siblings, ok)
}

// handleKey applies the root keymap; stop means the returned command quits the program.
func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()

	// The help overlay only answers to quit, to its own toggle and to esc.
	if m.help {
		action, ok := m.keys.Match(keymap.ScopeRoot, key)
		switch {
		case ok && action == keymap.ActionQuit:
			return m.quit()
		case ok && action == keymap.ActionHelp, key == "esc":
			m.help = false
		}

		return nil, false
	}

	// The configuration overlay only answers to quit, to its own toggle and to esc.
	if m.configOpen {
		action, ok := m.keys.Match(keymap.ScopeRoot, key)
		switch {
		case ok && action == keymap.ActionQuit:
			return m.quit()
		case ok && action == keymap.ActionConfig, key == "esc":
			m.configOpen = false
		}

		return nil, false
	}

	// The filter input owns every key but ctrl+c, which never leaves the UI stuck.
	if m.table.CapturesInput() {
		if key != "ctrl+c" {
			return m.forwardKey(msg), false
		}

		return m.quit()
	}

	action, ok := m.keys.Match(keymap.ScopeRoot, key)
	if !ok {
		return m.forwardKey(msg), false
	}

	switch action {
	case keymap.ActionQuit:
		return m.quit()
	case keymap.ActionHelp:
		m.help = true

		return nil, false
	case keymap.ActionConfig:
		m.configOpen = true

		return nil, false
	case keymap.ActionTabListen:
		return m.setTab(ListenTab), false
	case keymap.ActionTabConnected:
		return m.setTab(ConnectedTab), false
	case keymap.ActionTabAll:
		return m.setTab(AllTab), false
	case keymap.ActionToggleInspector:
		return m.toggleInspector(), false
	case keymap.ActionSplitShrink:
		return m.resizeSplit(-splitStep), false
	case keymap.ActionSplitGrow:
		return m.resizeSplit(splitStep), false
	case keymap.ActionFocusNext, keymap.ActionFocusPrev:
		m.toggleFocus()

		return nil, false
	}

	return m.forwardKey(msg), false
}

// forwardKey sends a key to the panel that owns the focus.
func (m *Model) forwardKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.focus == FocusInspector && !m.inspectorCollapsed {
		next, cmd := m.detail.Update(msg)
		m.detail = next

		return cmd
	}

	next, cmd := m.table.Update(msg)
	m.table = next

	return cmd
}

// handleClick routes a click to the tabs, the inspector button, the splitter or a row.
func (m *Model) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	if m.help || m.configOpen {
		return nil
	}

	splitter := m.onSplitter(msg.X, msg.Y)
	if !splitter {
		m.cancelDrag() // a click away from the handle never keeps a drag alive
	}

	if tab, ok := m.tabAt(msg.X, msg.Y); ok {
		return m.setTab(tab) // an active tab is a no-op: it never steals the focus
	}

	if m.onCollapse(msg.X, msg.Y) {
		return m.toggleInspector() // the button mirrors the toggle key and never moves the selection
	}

	if splitter {
		m.dragging, m.dragDirty = true, false

		return nil // resizing never moves the keyboard
	}

	panel, ok := m.panelAt(msg.X, msg.Y)
	if !ok {
		return nil
	}
	m.focus = panel

	if panel == FocusTable {
		next, cmd := m.table.Update(msg)
		m.table = next

		return cmd
	}

	return nil
}

// handleMotion resizes the table while the shared border is dragged.
func (m *Model) handleMotion(msg tea.MouseMotionMsg) tea.Cmd {
	if !m.dragging {
		return nil
	}

	if m.inspectorCollapsed {
		m.cancelDrag()

		return nil
	}

	width, _ := m.canvas()
	next := clamp(msg.X+1, minTable, max(width-minInspector, minTable))
	if next == m.tableWidthNow(width) {
		return nil
	}

	m.splitWidth = next
	m.dragDirty = true
	m.resize()

	return nil
}

// handleRelease ends the table drag and persists the new width once.
func (m *Model) handleRelease(msg tea.MouseReleaseMsg) tea.Cmd {
	if !m.dragging {
		return nil
	}

	m.dragging = false
	if !m.dragDirty {
		return nil
	}

	m.dragDirty = false
	save := m.saveSplit
	if save == nil {
		return nil
	}

	width, collapsed := m.splitWidth, m.inspectorCollapsed

	return func() tea.Msg { return layoutSavedMsg{err: save(width, collapsed)} }
}

// cancelDrag drops a drag without persisting anything.
func (m *Model) cancelDrag() {
	m.dragging, m.dragDirty = false, false
}

// onSplitter reports whether the point falls on the draggable border.
func (m Model) onSplitter(x, y int) bool {
	return m.canSplit() && image.Pt(x, y).In(m.splitterRect)
}

// onCollapse reports whether the point falls on the inspector button.
func (m Model) onCollapse(x, y int) bool {
	return image.Pt(x, y).In(m.collapseRect)
}

// handleWheel sends the wheel to the panel under the cursor, not to the focused one.
func (m *Model) handleWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if m.help || m.configOpen {
		return nil
	}

	panel, ok := m.panelAt(msg.X, msg.Y)
	if !ok {
		return nil
	}
	m.focus = panel

	if panel == FocusInspector {
		next, cmd := m.detail.Update(msg)
		m.detail = next

		return cmd
	}

	next, cmd := m.table.Update(msg)
	m.table = next

	return cmd
}

// tabAt maps terminal coordinates to the top tab that owns them.
func (m Model) tabAt(x, y int) (Tab, bool) {
	point := image.Pt(x, y)

	for tab := Tab(0); tab < tabCount; tab++ {
		if m.tabRects[tab].Dx() > 0 && point.In(m.tabRects[tab]) {
			return tab, true
		}
	}

	return m.tab, false
}

// panelAt maps terminal coordinates to the panel that owns them.
func (m Model) panelAt(x, y int) (Focus, bool) {
	point := image.Pt(x, y)

	if point.In(m.tableRect) {
		return FocusTable, true
	}
	if !m.inspectorCollapsed && point.In(m.inspectorRect) {
		return FocusInspector, true
	}

	return m.focus, false
}

// setTab switches the active filter and hands the focus to the table.
func (m *Model) setTab(tab Tab) tea.Cmd {
	if tab == m.tab {
		return nil
	}

	m.tab = tab
	m.table.SetScope(tab.Scope())
	m.focus = FocusTable
	m.resize()

	return nil
}

// toggleFocus alternates between the table and the detail pane.
func (m *Model) toggleFocus() {
	if m.inspectorCollapsed || m.focus == FocusInspector {
		m.focus = FocusTable

		return
	}

	m.focus = FocusInspector
}

// toggleInspector hides or shows the detail pane, leaving the keyboard in the table either way.
func (m *Model) toggleInspector() tea.Cmd {
	m.inspectorCollapsed = !m.inspectorCollapsed
	if m.inspectorCollapsed {
		m.cancelDrag() // the handle disappears together with the pane
	}
	m.focus = FocusTable
	m.resize()

	save := m.saveSplit
	if save == nil {
		return nil
	}

	width, collapsed := m.splitWidth, m.inspectorCollapsed

	return func() tea.Msg { return layoutSavedMsg{err: save(width, collapsed)} }
}

// resizeSplit moves the shared border by delta cells, clamped like the drag, and persists it once.
func (m *Model) resizeSplit(delta int) tea.Cmd {
	if m.inspectorCollapsed {
		return nil // a hidden pane has no border to move
	}

	width, _ := m.canvas()
	current := m.tableWidthNow(width)
	next := clamp(current+delta, minTable, max(width-minInspector, minTable))
	if next == current {
		return nil
	}

	m.splitWidth = next
	m.resize()

	save := m.saveSplit
	if save == nil {
		return nil
	}

	persisted, collapsed := m.splitWidth, m.inspectorCollapsed

	return func() tea.Msg { return layoutSavedMsg{err: save(persisted, collapsed)} }
}

// canSplit reports whether the layout shows a draggable splitter.
func (m Model) canSplit() bool {
	width, _ := m.canvas()
	tableW := m.tableWidthNow(width)

	return !m.inspectorCollapsed && tableW > 0 && width > tableW
}

// tableWidthNow returns the outer width of the table, or the full width while the pane is collapsed.
func (m Model) tableWidthNow(width int) int {
	if m.inspectorCollapsed {
		return width
	}

	return splitWidth(width, m.splitWidth)
}

// quit leaves the program.
func (m *Model) quit() (tea.Cmd, bool) {
	return tea.Quit, true
}

// notify shows a toast that does not come from a panel error.
func (m *Model) notify(text string) tea.Cmd {
	m.toast = toastPrefix + text
	m.toastSeq++
	seq := m.toastSeq

	return tea.Tick(toastTTL, func(time.Time) tea.Msg { return toastExpiredMsg{seq: seq} })
}

// afterUpdate reconciles the toast and keeps exactly one spinner tick in flight.
func (m *Model) afterUpdate() tea.Cmd {
	cmds := make([]tea.Cmd, 0, 2)

	if cmd := m.syncToast(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if m.loading() && !m.spinning {
		m.spinning = true
		cmds = append(cmds, m.spinner.Tick)
	}

	return tea.Batch(cmds...)
}

// syncToast shows a toast when the first error changes, and schedules its expiry.
func (m *Model) syncToast() tea.Cmd {
	err := m.currentError()
	if sameError(err, m.lastErr) {
		return nil
	}

	m.lastErr = err
	if err == nil {
		return nil
	}

	m.toast = toastPrefix + err.Error()
	m.toastSeq++
	seq := m.toastSeq

	return tea.Tick(toastTTL, func(time.Time) tea.Msg { return toastExpiredMsg{seq: seq} })
}

// currentError returns the first active error of the panels.
func (m Model) currentError() error {
	return m.table.Err()
}

// loading reports whether the table is still reading.
func (m Model) loading() bool {
	return m.table.Loading()
}

func sameError(a, b error) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Error() == b.Error()
	}
}

func (m Model) focusLabel() string {
	if m.focus == FocusInspector {
		return "detalle"
	}

	return "tabla"
}

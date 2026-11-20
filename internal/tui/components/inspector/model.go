// Package inspector renders the read-only detail of the socket selected in the
// ports table: the endpoint, the owning process and every port that process
// owns. The pane only scrolls; it never performs I/O.
package inspector

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-port/internal/domain"
	"github.com/MiguelFPP/tui-port/internal/tui/keymap"
)

const (
	defaultWidth  = 80
	defaultHeight = 20

	// headerRows is the title row and footerRows the hint row of the pane.
	headerRows = 1
	footerRows = 1

	// minBody is the smallest viewport height the pane keeps when collapsed.
	minBody = 1
)

// Model is the read-only detail of the selected socket. The root owns the
// selection, the keyboard focus and the frame drawn around the pane.
type Model struct {
	keys keymap.Registry

	socket   domain.Socket
	process  domain.Process
	siblings []domain.Socket
	ok       bool

	viewport    viewport.Model
	lines       []string
	cache       string
	selectionID string

	width  int
	height int
}

// New builds an empty pane; a zero registry falls back to the built-in defaults.
func New(keys keymap.Registry) Model {
	if len(keys.All()) == 0 {
		keys = keymap.Default()
	}

	m := Model{
		keys:     keys,
		viewport: viewport.New(),
	}
	m.viewport.FillHeight = true
	m.viewport.MouseWheelEnabled = true
	// The pane wraps the command line itself, so the viewport must count one row per line.
	m.viewport.SoftWrap = false
	m.SetBounds(0, 0, defaultWidth, defaultHeight)

	return m
}

// SetBounds stores the size the root gave the pane and rebuilds the content
// when the new width changes the rendering.
func (m *Model) SetBounds(x, y, width, height int) {
	m.width, m.height = max(width, 0), max(height, 0)

	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(m.bodyHeight())
	// A taller pane shrinks the scrollable range: clamp instead of leaving the offset past the content.
	m.viewport.SetYOffset(m.viewport.YOffset())
	m.refresh()
}

// bodyHeight is the number of viewport rows between the title and the footer.
func (m Model) bodyHeight() int {
	return max(m.height-headerRows-footerRows, minBody)
}

// SetSelection replaces the selection with the data the root read. It is a
// no-op when nothing that affects the rendered content changed, so the root may
// call it on every update; the lines and the viewport content are rebuilt only
// when the digest changes, and the scroll only resets on a new socket.
func (m *Model) SetSelection(socket domain.Socket, process domain.Process, siblings []domain.Socket, ok bool) {
	key := selectionKey(ok, socket, process, siblings, m.width)
	if key == m.cache {
		return
	}

	m.ok = ok
	m.socket = socket
	m.process = process
	m.siblings = slices.Clone(siblings)
	m.cache = key
	m.rebuild()
}

// Update applies one message. Only the inspector keys and the wheel move the
// pane; the root owns the bounds, so a window size message is ignored.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m.handleKey(msg)

		return m, nil
	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)

		return m, cmd
	default:
		return m, nil
	}
}

// handleKey scrolls the viewport with the actions of the inspector scope.
func (m *Model) handleKey(msg tea.KeyPressMsg) {
	action, ok := m.keys.Match(keymap.ScopeInspector, msg.String())
	if !ok {
		return
	}

	switch action {
	case keymap.ActionInspectorUp:
		m.viewport.ScrollUp(1)
	case keymap.ActionInspectorDown:
		m.viewport.ScrollDown(1)
	case keymap.ActionInspectorPageUp:
		m.viewport.PageUp()
	case keymap.ActionInspectorPageDown:
		m.viewport.PageDown()
	case keymap.ActionInspectorHome:
		m.viewport.GotoTop()
	case keymap.ActionInspectorEnd:
		m.viewport.GotoBottom()
	}
}

// selectionKey digests everything the rendered content depends on, plus the
// content width, so a selection that would render the same is skipped.
func selectionKey(ok bool, socket domain.Socket, process domain.Process, siblings []domain.Socket, width int) string {
	var b strings.Builder
	b.Grow(128 + len(siblings)*48)

	fmt.Fprintf(&b, "%t|%d", ok, width)
	if ok {
		fmt.Fprintf(&b, "|%s|%s|%s|%s|%d|%d|%d", socket.ID(), socket.Local.String(), socket.Remote.String(),
			socket.State.String(), socket.UID, socket.Inode, socket.PID)
		fmt.Fprintf(&b, "|%d|%d|%q|%q|%q|%d|%d|%d", process.PID, process.PPID, process.Name, process.User,
			process.Cmdline, process.RSS, process.Threads, process.StartedAt.UnixNano())
	}

	fmt.Fprintf(&b, "|%d", len(siblings))
	for _, sibling := range siblings {
		fmt.Fprintf(&b, "|%s|%s|%s", sibling.ID(), sibling.Local.String(), sibling.State.String())
	}

	return b.String()
}

// refresh rebuilds the content when the stored selection or the width changed.
func (m *Model) refresh() {
	key := selectionKey(m.ok, m.socket, m.process, m.siblings, m.width)
	if key == m.cache {
		return
	}

	m.cache = key
	m.rebuild()
}

// rebuild renders the lines of the current selection and resets the scroll only
// when a different socket is selected.
func (m *Model) rebuild() {
	selected := ""
	if m.ok {
		selected = m.socket.ID()
	}

	reset := selected != m.selectionID
	m.selectionID = selected

	if !m.ok {
		m.lines = nil
		m.viewport.SetContent("")

		return
	}

	m.lines = m.contentLines()
	m.viewport.SetContent(strings.Join(m.lines, "\n"))
	if reset {
		m.viewport.SetYOffset(0)
	}
}

// attributed reports whether the snapshot resolved an owner for the socket.
func (m Model) attributed() bool {
	return m.socket.PID > 0 && m.process.PID > 0
}

// sortedSiblings orders the ports of the process by local port and protocol.
func sortedSiblings(siblings []domain.Socket) []domain.Socket {
	if len(siblings) == 0 {
		return nil
	}

	ordered := slices.Clone(siblings)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Local.Port != ordered[j].Local.Port {
			return ordered[i].Local.Port < ordered[j].Local.Port
		}

		return ordered[i].Protocol < ordered[j].Protocol
	})

	return ordered
}

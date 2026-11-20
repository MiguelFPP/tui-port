package porttable

import (
	"context"
	"image"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-port/internal/domain"
	"github.com/MiguelFPP/tui-port/internal/ports"
	"github.com/MiguelFPP/tui-port/internal/tui/keymap"
)

// Init starts the first snapshot load and arms the always-on ticker. An answer
// of this load is applied by the first Update of the snapshotMsg it produces.
func (m Model) Init() tea.Cmd {
	return tea.Batch(loadSnapshot(m.monitor, m.snapshotSeq), m.scheduleTick())
}

// Update applies one message; every read and every signal goes through a
// command, never inline.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case snapshotMsg:
		m.applySnapshot(msg)
	case tickMsg:
		return m, m.onTick()
	case actionMsg:
		return m, m.applyAction(msg)
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	case tea.MouseClickMsg:
		return m, m.handleClick(msg)
	case tea.MouseWheelMsg:
		return m, m.handleWheel(msg)
	case tea.PasteMsg:
		if m.editing {
			return m, m.updateInput(msg)
		}
	}

	return m, nil
}

// scheduleTick arms the next tick of the ticker; the ticker always runs.
func (m Model) scheduleTick() tea.Cmd {
	return tea.Tick(m.interval, func(time.Time) tea.Msg { return tickMsg{} })
}

// onTick reloads at most one snapshot per period and re-arms the ticker; a load
// already in flight is never stacked with a second one.
func (m *Model) onTick() tea.Cmd {
	commands := make([]tea.Cmd, 0, 2)
	if !m.loading {
		commands = append(commands, m.startLoad())
	}
	commands = append(commands, m.scheduleTick())

	return tea.Batch(commands...)
}

// startLoad asks for one snapshot, leaving the previous one in place until a
// newer answer arrives.
func (m *Model) startLoad() tea.Cmd {
	m.snapshotSeq++
	m.loading = true
	m.err = nil

	return loadSnapshot(m.monitor, m.snapshotSeq)
}

// applySnapshot stores one reading; a late answer of an older request is
// dropped and a failure keeps the last good snapshot on screen.
func (m *Model) applySnapshot(msg snapshotMsg) {
	if msg.seq != m.snapshotSeq {
		return
	}

	m.loading = false
	if msg.err != nil {
		m.err = msg.err

		return
	}

	id, hadSelection := m.selectedID()

	m.err = nil
	m.loaded = true
	m.sockets = append(m.sockets[:0], msg.snapshot.Sockets...)
	m.processes, m.byPID = indexSnapshot(msg.snapshot)
	m.counts = msg.snapshot.Counts()

	m.rebuildRows()
	if hadSelection {
		m.focusByID(id)
	}

	m.sync()
}

// handleKey gives the keyboard to the filter while it is being edited and
// resolves everything else against the ports scope of the registry.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.editing {
		return m.handleFilterKey(msg)
	}

	action, ok := m.keys.Match(keymap.ScopePorts, msg.String())
	if !ok {
		return nil
	}

	switch action {
	case keymap.ActionPortsUp:
		m.moveCursor(-1)
	case keymap.ActionPortsDown:
		m.moveCursor(1)
	case keymap.ActionPortsPageUp:
		m.moveCursor(-m.bodyHeight())
	case keymap.ActionPortsPageDown:
		m.moveCursor(m.bodyHeight())
	case keymap.ActionPortsHome:
		m.moveCursor(-len(m.rows))
	case keymap.ActionPortsEnd:
		m.moveCursor(len(m.rows))
	case keymap.ActionPortsSort:
		m.cycleSort()
	case keymap.ActionPortsFilter:
		return m.startFilter()
	case keymap.ActionPortsClear:
		m.clearFilter()
	case keymap.ActionPortsKill:
		return m.startAction(domain.SignalKill)
	case keymap.ActionPortsTerm:
		return m.startAction(domain.SignalTerm)
	case keymap.ActionPortsRefresh:
		return m.startLoad()
	}

	return nil
}

// handleFilterKey drives the filter input: enter keeps the text and closes it,
// esc clears it and closes it, every other key edits it.
func (m *Model) handleFilterKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		return m.closeFilter(false)
	case "esc":
		return m.closeFilter(true)
	}

	return m.updateInput(msg)
}

// closeFilter leaves the input, drops the filter when asked, and reflows the body.
func (m *Model) closeFilter(clear bool) tea.Cmd {
	m.editing = false
	m.input.Blur()

	if clear {
		m.setFilter("") // it synces on its own when the text changed
	}

	m.sync()

	return nil
}

// updateInput forwards one message to the filter input and re-filters the table
// when its value changed.
func (m *Model) updateInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd

	m.input, cmd = m.input.Update(msg)
	m.setFilter(m.input.Value())

	return cmd
}

// setFilter stores the filter and rebuilds the table, keeping the selection.
func (m *Model) setFilter(filter string) {
	if m.filter == filter {
		return
	}

	id, hadSelection := m.selectedID()

	m.filter = filter
	m.needle = strings.ToLower(filter)
	if m.input.Value() != filter {
		m.input.SetValue(filter)
	}

	m.rebuildRows()
	if hadSelection {
		m.focusByID(id)
	}

	m.sync()
}

// startFilter opens the filter input over the table with the current text. The
// extra line cuts one row from the body, so the window is reflowed here.
func (m *Model) startFilter() tea.Cmd {
	m.editing = true
	m.input.SetValue(m.filter)
	m.input.CursorEnd()
	m.input.Focus() // the cursor is static, so there is no blink command to run
	m.sync()

	return nil
}

// clearFilter drops the active filter and rebuilds the table.
func (m *Model) clearFilter() {
	if m.filter == "" {
		return
	}

	m.setFilter("")
}

// cycleSort advances to the next order and rebuilds the table.
func (m *Model) cycleSort() {
	id, hadSelection := m.selectedID()

	m.order = m.order.next()
	m.rebuildRows()

	if hadSelection {
		m.focusByID(id)
	}

	m.sync()
}

// moveCursor moves the selection and keeps it inside the window.
func (m *Model) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}

	m.cursor = min(max(m.cursor+delta, 0), len(m.rows)-1)
	m.scrollToCursor()
	m.markRows()
	m.syncSelection()
}

// startAction signals the process of the selected socket; a socket without an
// owner and the processes the monitor must never touch are refused here.
func (m *Model) startAction(signal domain.Signal) tea.Cmd {
	if m.writing || !m.hasSelection {
		return nil
	}

	socket := m.selected
	if !socket.Attributed() {
		m.notice = noticeNoProcess

		return nil
	}

	pid := socket.PID
	if pid <= 1 || pid == os.Getpid() {
		m.notice = noticeProtected

		return nil
	}

	name := m.rows[m.cursor].name()

	m.actionSeq++
	m.writing = true
	m.notice = ""
	m.actionErr = nil

	return actionCmd(m.monitor, m.actionSeq, pid, name, signal)
}

// applyAction stores the outcome of one signal: a success leaves a notice and
// reloads the sockets, a failure keeps the error for the footer.
func (m *Model) applyAction(msg actionMsg) tea.Cmd {
	if msg.seq != m.actionSeq {
		return nil
	}

	m.writing = false

	if msg.err != nil {
		m.actionErr = msg.err

		return nil
	}

	m.actionErr = nil
	m.notice = noticeOf(msg)

	return m.startLoad()
}

// noticeOf is the confirmation left in the footer by a successful signal.
func noticeOf(msg actionMsg) string {
	prefix := "kill -9 enviado a "
	if msg.signal == domain.SignalTerm {
		prefix = "SIGTERM enviado a "
	}

	return prefix + msg.name + " (pid " + strconv.Itoa(msg.pid) + ")"
}

// handleClick selects the row under the pointer; a click never signals anything.
func (m *Model) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}

	row, ok := m.rowAt(image.Pt(msg.X, msg.Y))
	if !ok {
		return nil
	}

	m.cursor = row
	m.scrollToCursor()
	m.markRows()
	m.syncSelection()

	return nil
}

// handleWheel scrolls the window without moving the selection.
func (m *Model) handleWheel(msg tea.MouseWheelMsg) tea.Cmd {
	switch msg.Button {
	case tea.MouseWheelUp:
		m.offset -= wheelStep
	case tea.MouseWheelDown:
		m.offset += wheelStep
	default:
		return nil
	}

	m.offset = min(max(m.offset, 0), max(len(m.rows)-m.bodyHeight(), 0))
	m.markRows()

	return nil
}

// loadSnapshot reads the sockets of the host under a bounded context.
func loadSnapshot(monitor ports.MonitorService, seq int) tea.Cmd {
	if monitor == nil {
		return func() tea.Msg { return snapshotMsg{seq: seq, err: domain.ErrUnavailable} }
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), snapshotTimeout)
		defer cancel()

		snapshot, err := monitor.Snapshot(ctx)

		return snapshotMsg{seq: seq, snapshot: snapshot, err: err}
	}
}

// actionCmd signals one process under a bounded context.
func actionCmd(monitor ports.MonitorService, seq, pid int, name string, signal domain.Signal) tea.Cmd {
	if monitor == nil {
		return func() tea.Msg {
			return actionMsg{seq: seq, pid: pid, name: name, signal: signal, err: domain.ErrUnavailable}
		}
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()

		err := monitor.Kill(ctx, pid, signal)

		return actionMsg{seq: seq, pid: pid, name: name, signal: signal, err: err}
	}
}

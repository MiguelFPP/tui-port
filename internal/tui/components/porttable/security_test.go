package porttable

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MiguelFPP/tui-port/internal/domain"
)

func TestProcessNameCannotInjectTerminalSequences(t *testing.T) {
	snapshot := sampleSnapshot()
	for i := range snapshot.Processes {
		if snapshot.Processes[i].PID == 100 {
			snapshot.Processes[i].Name = "\x1b]52;c;SGVtbw==\x07nginx"
		}
	}

	monitor := newFakeMonitor(snapshot)
	m := feed(t, testModel(t, monitor), snapshotMsg{seq: 0, snapshot: snapshot})
	monitor.reset()

	var name string
	for _, item := range m.rows {
		if item.socket.PID == 100 {
			name = item.name()
		}
	}
	if strings.ContainsAny(name, "\x1b\x07") {
		t.Fatalf("row name = %q, want the terminal sequences neutralized", name)
	}
	if !strings.Contains(name, "nginx") {
		t.Fatalf("row name = %q, want the printable suffix preserved", name)
	}

	view := plain(m.View())
	if strings.Contains(view, "\x1b]52") || strings.Contains(view, "\x07") {
		t.Fatalf("View() leaked a terminal sequence: %q", view)
	}

	next, cmd := m.Update(typed('x'))
	if cmd == nil {
		t.Fatal("x did not send a signal")
	}

	next, _, _ = applyMessages(t, next, cmd)
	if strings.ContainsAny(next.notice, "\x1b\x07") {
		t.Fatalf("notice = %q, want the terminal sequences neutralized", next.notice)
	}
	if got := plain(next.View()); strings.Contains(got, "\x1b]52") {
		t.Fatalf("the notice leaked a terminal sequence: %q", got)
	}
}

func TestUnverifiedProcessIsNotSignaled(t *testing.T) {
	snapshot := sampleSnapshot()
	for i := range snapshot.Processes {
		if snapshot.Processes[i].PID == 100 {
			snapshot.Processes[i].StartTicks = 0
		}
	}

	monitor := newFakeMonitor(snapshot)
	m := feed(t, testModel(t, monitor), snapshotMsg{seq: 0, snapshot: snapshot})
	monitor.reset()

	next, cmd := m.Update(typed('x'))
	if cmd != nil {
		t.Fatal("x returned a command for a process without a verified start time")
	}
	if next.notice != noticeUnverified {
		t.Errorf("notice = %q, want %q", next.notice, noticeUnverified)
	}
	if got := monitor.killCalls(); len(got) != 0 {
		t.Errorf("Kill calls = %+v, want none", got)
	}
}

func TestRecycledProcessIsReported(t *testing.T) {
	monitor := newFakeMonitor(sampleSnapshot())
	monitor.killErr = fmt.Errorf("%w: pid 100", domain.ErrProcessChanged)
	m := loadedModel(t, monitor)
	monitor.reset()

	next, cmd := m.Update(typed('x'))
	if cmd == nil {
		t.Fatal("x did not send a signal")
	}

	next, _, _ = applyMessages(t, next, cmd)
	if !errors.Is(next.Err(), domain.ErrProcessChanged) {
		t.Fatalf("Err() = %v, want ErrProcessChanged", next.Err())
	}
	if got := plain(next.View()); !strings.Contains(got, "el proceso cambió; refresca la tabla") {
		t.Errorf("View() = %q, want the recycled PID explanation", got)
	}
	if got := monitor.snapshotCalls(); got != 0 {
		t.Errorf("Snapshot calls = %d, want none after a refused signal", got)
	}
}

func TestFilterAndErrorsCannotInjectTerminalSequences(t *testing.T) {
	m := loadedModel(t, newFakeMonitor(sampleSnapshot()))

	m.setFilter("\x1b]0;owned\x07")
	if view := plain(m.View()); strings.Contains(view, "\x1b]0;") {
		t.Fatalf("the filter line leaked a terminal sequence: %q", view)
	}

	m.actionErr = errors.New("fallo \x1b]0;owned\x07")
	if view := plain(m.View()); strings.Contains(view, "\x1b]0;") {
		t.Fatalf("the footer leaked a terminal sequence: %q", view)
	}
}

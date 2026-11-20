package porttable

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-port/internal/domain"
	"github.com/MiguelFPP/tui-port/internal/tui/keymap"
)

// messagesOf runs cmd and counts the answers of the given kind, feeding the
// snapshot answers back into the model.
func applyMessages(t *testing.T, m Model, cmd tea.Cmd) (Model, int, int) {
	t.Helper()

	loads, actions := 0, 0
	for _, msg := range collect(t, cmd) {
		switch msg.(type) {
		case snapshotMsg:
			loads++
		case actionMsg:
			actions++
		}

		m = feed(t, m, msg)
	}

	return m, loads, actions
}

func TestInitSchedulesALoadAndATick(t *testing.T) {
	monitor := newFakeMonitor(sampleSnapshot())
	m := testModel(t, monitor)

	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init() = nil, want the load and the first tick")
	}

	m, loads, actions := applyMessages(t, m, m.Init())
	if loads != 1 {
		t.Errorf("Init() requested %d snapshots, want 1", loads)
	}
	if actions != 0 {
		t.Errorf("Init() returned %d actions, want none", actions)
	}
	if got := monitor.snapshotCalls(); got != 1 {
		t.Errorf("Snapshot calls = %d, want 1", got)
	}
	if got := len(m.rows); got != 6 {
		t.Errorf("rows = %d, want the snapshot applied by Init", got)
	}
	if m.Loading() {
		t.Error("Loading() = true after the first answer")
	}

	// The tick is part of the batch: the same command must produce one.
	ticks := 0
	for _, msg := range collect(t, m.Init()) {
		if _, ok := msg.(tickMsg); ok {
			ticks++
		}
	}
	if ticks != 1 {
		t.Errorf("Init() armed %d ticks, want 1", ticks)
	}
}

func TestInitWithoutMonitorReportsUnavailable(t *testing.T) {
	m := New(Deps{Keys: keymap.Default(), Interval: time.Nanosecond})
	m.SetBounds(0, 0, 100, 24)

	var errs []error
	for _, msg := range collect(t, m.Init()) {
		if typed, ok := msg.(snapshotMsg); ok {
			errs = append(errs, typed.err)
		}

		m = feed(t, m, msg)
	}

	if len(errs) != 1 || !errors.Is(errs[0], domain.ErrUnavailable) {
		t.Fatalf("Init() errors = %v, want one ErrUnavailable", errs)
	}
	if !errors.Is(m.Err(), domain.ErrUnavailable) {
		t.Fatalf("Err() = %v, want ErrUnavailable", m.Err())
	}
	if got := plain(m.View()); !strings.Contains(got, "monitor no disponible") {
		t.Errorf("View() = %q, want the unavailable state", got)
	}
}

func TestTickReloadsAndRearms(t *testing.T) {
	monitor := newFakeMonitor(sampleSnapshot())
	m := loadedModel(t, monitor)
	monitor.reset()

	next, cmd := m.Update(tickMsg{})
	if cmd == nil {
		t.Fatal("a tick must re-arm the ticker")
	}

	ticks, loads := 0, 0
	for _, msg := range collect(t, cmd) {
		switch msg.(type) {
		case tickMsg:
			ticks++
		case snapshotMsg:
			loads++
		}

		next = feed(t, next, msg)
	}

	if ticks != 1 {
		t.Errorf("a tick armed %d ticks, want 1", ticks)
	}
	if loads != 1 {
		t.Errorf("a tick requested %d snapshots, want 1", loads)
	}
	if got := monitor.snapshotCalls(); got != 1 {
		t.Errorf("Snapshot calls = %d, want exactly one per tick", got)
	}
	if got := len(next.rows); got != 6 {
		t.Errorf("rows = %d, want the snapshot kept", got)
	}
	if next.loading {
		t.Error("loading = true after the answer of the tick")
	}
}

func TestTickSkipsWhileALoadIsInFlight(t *testing.T) {
	monitor := newFakeMonitor(sampleSnapshot())
	m := loadedModel(t, monitor)
	monitor.reset()

	inFlight, cmd := m.Update(typed('r'))
	if cmd == nil {
		t.Fatal("r did not start a load")
	}
	if !inFlight.loading {
		t.Fatal("loading = false after starting a load")
	}

	_, tickCmd := inFlight.Update(tickMsg{})
	if tickCmd == nil {
		t.Fatal("a tick must re-arm the ticker even while loading")
	}

	for _, msg := range collect(t, tickCmd) {
		if _, ok := msg.(snapshotMsg); ok {
			t.Fatal("a tick started a load while another one was in flight")
		}
	}
	if got := monitor.snapshotCalls(); got != 0 {
		t.Errorf("Snapshot calls = %d, want 0 while a load is in flight", got)
	}
}

func TestStaleSnapshotIsDropped(t *testing.T) {
	monitor := newFakeMonitor(sampleSnapshot())
	m := loadedModel(t, monitor)

	inFlight, _ := m.Update(typed('r'))
	late := feed(t, inFlight, snapshotMsg{seq: 0, snapshot: domain.Snapshot{}})

	if got := len(late.rows); got != 6 {
		t.Errorf("rows = %d, want the previous snapshot kept", got)
	}
	if late.Err() != nil {
		t.Errorf("Err() = %v, want nil", late.Err())
	}
	if !late.loading {
		t.Error("loading = false, want the fresh request still pending")
	}
}

func TestRefreshRetriesAfterAFailure(t *testing.T) {
	monitor := newFakeMonitor(sampleSnapshot())
	monitor.snapshotErr = errors.New("boom")
	m := testModel(t, monitor)

	m = feed(t, m, typed('r'))
	if m.Err() == nil {
		t.Fatal("Err() = nil, want the failure of the load")
	}
	if m.loaded {
		t.Error("loaded = true after a failed load")
	}
	if got := plain(m.View()); !strings.Contains(got, "error: boom") {
		t.Errorf("View() = %q, want the failure", got)
	}

	monitor.snapshotErr = nil
	m = feed(t, m, typed('r'))

	if m.Err() != nil {
		t.Fatalf("Err() = %v after a good load, want nil", m.Err())
	}
	if got := len(m.rows); got != 6 {
		t.Errorf("rows = %d, want the snapshot of the retry", got)
	}
}

func TestKillKeySignalsAndNotifies(t *testing.T) {
	monitor := newFakeMonitor(sampleSnapshot())
	m := loadedModel(t, monitor)
	monitor.reset()

	next, cmd := m.Update(typed('x'))
	if cmd == nil {
		t.Fatal("x did not send a signal")
	}
	if !next.writing {
		t.Error("writing = false while the signal is in flight")
	}
	if next.notice != "" {
		t.Errorf("notice = %q, want it cleared by the new action", next.notice)
	}
	if got := plain(next.View()); !strings.Contains(got, "enviando señal…") {
		t.Errorf("View() = %q, want the pending action", got)
	}

	next, _, actions := applyMessages(t, next, cmd)
	if actions != 1 {
		t.Fatalf("x sent %d actions, want 1", actions)
	}

	calls := monitor.killCalls()
	if len(calls) != 1 || calls[0].pid != 100 || calls[0].signal != domain.SignalKill {
		t.Fatalf("Kill calls = %+v, want SIGKILL to the nginx pid", calls)
	}
	if next.writing {
		t.Error("writing = true after the answer")
	}

	want := "kill -9 enviado a nginx (pid 100)"
	if next.notice != want {
		t.Errorf("notice = %q, want %q", next.notice, want)
	}
	if got := plain(next.View()); !strings.Contains(got, want) {
		t.Errorf("View() = %q, want the notice in the footer", got)
	}
	if got := monitor.snapshotCalls(); got != 1 {
		t.Errorf("Snapshot calls = %d, want the reload after the signal", got)
	}
	if next.Err() != nil {
		t.Errorf("Err() = %v, want nil", next.Err())
	}
}

func TestTermKeySendsSIGTERM(t *testing.T) {
	monitor := newFakeMonitor(sampleSnapshot())
	m := loadedModel(t, monitor)
	monitor.reset()

	next, cmd := m.Update(typed('t'))
	if cmd == nil {
		t.Fatal("t did not send a signal")
	}

	next, _, _ = applyMessages(t, next, cmd)

	calls := monitor.killCalls()
	if len(calls) != 1 || calls[0].signal != domain.SignalTerm {
		t.Fatalf("Kill calls = %+v, want SIGTERM", calls)
	}

	want := "SIGTERM enviado a nginx (pid 100)"
	if next.notice != want {
		t.Errorf("notice = %q, want %q", next.notice, want)
	}
	if got := plain(next.View()); !strings.Contains(got, want) {
		t.Errorf("View() = %q, want the notice in the footer", got)
	}
}

func TestSignalGuards(t *testing.T) {
	t.Run("without selection", func(t *testing.T) {
		m := feed(t, testModel(t, newFakeMonitor(domain.Snapshot{})), snapshotMsg{seq: 0, snapshot: domain.Snapshot{}})

		if _, cmd := m.Update(typed('x')); cmd != nil {
			t.Error("x without a selection returned a command")
		}
	})

	t.Run("without an owner", func(t *testing.T) {
		orphan := domain.Snapshot{
			Sockets: []domain.Socket{{
				Protocol: domain.ProtocolUDP,
				Local:    domain.Endpoint{Addr: "0.0.0.0", Port: 5353},
				State:    domain.StateListen,
				Inode:    9,
			}},
		}
		monitor := newFakeMonitor(orphan)
		m := feed(t, testModel(t, monitor), snapshotMsg{seq: 0, snapshot: orphan})
		monitor.reset()

		next, cmd := m.Update(typed('x'))
		if cmd != nil {
			t.Error("x on a socket without an owner returned a command")
		}
		if next.notice != noticeNoProcess {
			t.Errorf("notice = %q, want %q", next.notice, noticeNoProcess)
		}
		if got := monitor.killCalls(); len(got) != 0 {
			t.Errorf("Kill calls = %+v, want none", got)
		}
	})

	t.Run("protected pids", func(t *testing.T) {
		for _, pid := range []int{1, os.Getpid()} {
			snapshot := domain.Snapshot{
				Processes: []domain.Process{{PID: pid, Name: "guard", RSS: mib}},
				Sockets: []domain.Socket{{
					Protocol: domain.ProtocolTCP,
					Local:    domain.Endpoint{Addr: "127.0.0.1", Port: 9},
					State:    domain.StateListen,
					Inode:    8,
					PID:      pid,
				}},
			}
			monitor := newFakeMonitor(snapshot)
			m := feed(t, testModel(t, monitor), snapshotMsg{seq: 0, snapshot: snapshot})
			monitor.reset()

			next, cmd := m.Update(typed('x'))
			if cmd != nil {
				t.Errorf("pid %d: x returned a command", pid)
			}
			if next.notice != noticeProtected {
				t.Errorf("pid %d: notice = %q, want %q", pid, next.notice, noticeProtected)
			}
			if got := monitor.killCalls(); len(got) != 0 {
				t.Errorf("pid %d: Kill calls = %+v, want none", pid, got)
			}
		}
	})
}

func TestSignalFailureIsReported(t *testing.T) {
	monitor := newFakeMonitor(sampleSnapshot())
	monitor.killErr = fmt.Errorf("%w: sin permiso", domain.ErrPermissionDenied)
	m := loadedModel(t, monitor)
	monitor.reset()

	next, cmd := m.Update(typed('x'))
	if cmd == nil {
		t.Fatal("x did not send a signal")
	}

	next, _, _ = applyMessages(t, next, cmd)

	if !errors.Is(next.Err(), domain.ErrPermissionDenied) {
		t.Fatalf("Err() = %v, want ErrPermissionDenied", next.Err())
	}
	if next.writing {
		t.Error("writing = true after the failure")
	}
	if next.notice != "" {
		t.Errorf("notice = %q, want none after a failure", next.notice)
	}
	if got := plain(next.View()); !strings.Contains(got, "error: permiso denegado") {
		t.Errorf("View() = %q, want the failure in the footer", got)
	}
	if got := monitor.snapshotCalls(); got != 0 {
		t.Errorf("Snapshot calls = %d, want none after a failure", got)
	}

	// The next signal clears the failure.
	monitor.killErr = nil
	next, cmd = next.Update(typed('t'))
	next, _, _ = applyMessages(t, next, cmd)

	if next.Err() != nil {
		t.Errorf("Err() = %v, want it cleared by the new action", next.Err())
	}
}

func TestStaleActionIsDropped(t *testing.T) {
	monitor := newFakeMonitor(sampleSnapshot())
	m := loadedModel(t, monitor)

	inFlight, _ := m.Update(typed('x'))
	if !inFlight.writing {
		t.Fatal("writing = false after starting the signal")
	}

	late := feed(t, inFlight, actionMsg{seq: 0, pid: 100, name: "nginx", signal: domain.SignalKill})

	if !late.writing {
		t.Error("writing = false, want the action in flight kept")
	}
	if late.notice != "" {
		t.Errorf("notice = %q, want none from a stale answer", late.notice)
	}
	if late.Err() != nil {
		t.Errorf("Err() = %v, want nil", late.Err())
	}
}

func TestKeymapDrivesTheTable(t *testing.T) {
	keys, err := keymap.Default().Apply(map[string][]string{
		string(keymap.ActionPortsDown):     {"n"},
		string(keymap.ActionPortsSort):     {"o"},
		string(keymap.ActionPortsRefresh):  {"f5"},
		string(keymap.ActionPortsClear):    {"q"},
		string(keymap.ActionPortsFilter):   {"f"},
		string(keymap.ActionPortsKill):     {"K"},
		string(keymap.ActionPortsTerm):     {"T"},
		string(keymap.ActionPortsUp):       {"p"},
		string(keymap.ActionPortsHome):     {"f1"},
		string(keymap.ActionPortsEnd):      {"f2"},
		string(keymap.ActionPortsPageUp):   {"f3"},
		string(keymap.ActionPortsPageDown): {"f4"},
	})
	if err != nil {
		t.Fatalf("Apply() = %v, want no error", err)
	}

	monitor := newFakeMonitor(sampleSnapshot())
	m := New(Deps{Monitor: monitor, Keys: keys, Interval: time.Nanosecond})
	m.SetBounds(0, 0, 100, 24)
	m = feed(t, m, snapshotMsg{seq: 0, snapshot: sampleSnapshot()})

	m = feed(t, m, typed('n'))
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want the rebound down key to move it", m.cursor)
	}

	m = feed(t, m, typed('o'))
	if got := plain(m.View()); !strings.Contains(got, "orden: rss") {
		t.Errorf("View() = %q, want the rebound sort key to cycle", got)
	}

	monitor.reset()
	m = feed(t, m, key(tea.KeyF5))
	if got := monitor.snapshotCalls(); got != 1 {
		t.Errorf("Snapshot calls = %d, want the rebound refresh key to reload", got)
	}

	filtering := feed(t, m, typed('f'))
	if !filtering.CapturesInput() {
		t.Error("the rebound filter key did not open the input")
	}
}

// deadlineMonitor fails every call that reaches it without a bounded context, to
// prove the commands hand one to the port.
type deadlineMonitor struct{}

func (deadlineMonitor) Snapshot(ctx context.Context) (domain.Snapshot, error) {
	if _, ok := ctx.Deadline(); !ok {
		return domain.Snapshot{}, errors.New("no deadline")
	}

	return sampleSnapshot(), nil
}

func (deadlineMonitor) Kill(ctx context.Context, _ int, _ domain.Signal) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("no deadline")
	}

	return nil
}

func TestCommandsUseABoundedContext(t *testing.T) {
	m := testModel(t, deadlineMonitor{})

	m = feed(t, m, typed('r'))
	if m.Err() != nil {
		t.Fatalf("Err() = %v, want the load to run under a deadline", m.Err())
	}
	if got := len(m.rows); got != 6 {
		t.Fatalf("rows = %d, want the snapshot", got)
	}

	next, cmd := m.Update(typed('x'))
	if cmd == nil {
		t.Fatal("x did not send a signal")
	}

	next, _, _ = applyMessages(t, next, cmd)
	if next.Err() != nil {
		t.Errorf("Err() = %v, want the signal to run under a deadline", next.Err())
	}
}

package porttable

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-port/internal/domain"
	"github.com/MiguelFPP/tui-port/internal/ports"
	"github.com/MiguelFPP/tui-port/internal/tui/keymap"
)

const (
	kib = 1024
	mib = 1024 * kib
)

// killCall is one signal the fake monitor received.
type killCall struct {
	target domain.ProcessRef
	signal domain.Signal
}

// fakeMonitor is an in-memory ports.MonitorService: the panel never needs a real
// daemon to run its tests.
type fakeMonitor struct {
	mu sync.Mutex

	snapshot    domain.Snapshot
	snapshotErr error
	killErr     error

	snapshots int
	kills     []killCall
}

var _ ports.MonitorService = (*fakeMonitor)(nil)

func newFakeMonitor(snapshot domain.Snapshot) *fakeMonitor {
	return &fakeMonitor{snapshot: snapshot}
}

func (f *fakeMonitor) Snapshot(ctx context.Context) (domain.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return domain.Snapshot{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.snapshots++

	return f.snapshot, f.snapshotErr
}

func (f *fakeMonitor) Kill(ctx context.Context, target domain.ProcessRef, signal domain.Signal) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.kills = append(f.kills, killCall{target: target, signal: signal})

	return f.killErr
}

// snapshotCalls is how many readings the panel asked for.
func (f *fakeMonitor) snapshotCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.snapshots
}

// killCalls is a copy of the signals the panel sent.
func (f *fakeMonitor) killCalls() []killCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]killCall(nil), f.kills...)
}

// setSnapshot replaces the reading the next load returns.
func (f *fakeMonitor) setSnapshot(snapshot domain.Snapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.snapshot = snapshot
}

// reset forgets the recorded calls.
func (f *fakeMonitor) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.snapshots = 0
	f.kills = nil
}

// sampleSnapshot is a tiny host: three processes, one socket without an owner
// and a listener plus an established socket on the same port.
func sampleSnapshot() domain.Snapshot {
	return domain.Snapshot{
		TakenAt: time.Unix(1700000000, 0),
		Processes: []domain.Process{
			{PID: 100, Name: "nginx", User: "www-data", RSS: 12 * mib, StartTicks: 500},
			{PID: 200, Name: "postgres", User: "postgres", RSS: 256 * mib, StartTicks: 600},
			{PID: 300, Name: "node", User: "dev", RSS: 90 * kib, StartTicks: 700},
		},
		Sockets: []domain.Socket{
			{Protocol: domain.ProtocolTCP, Local: domain.Endpoint{Addr: "0.0.0.0", Port: 80}, State: domain.StateListen, Inode: 11, PID: 100},
			{Protocol: domain.ProtocolTCP, Local: domain.Endpoint{Addr: "127.0.0.1", Port: 5432}, State: domain.StateListen, Inode: 22, PID: 200},
			{
				Protocol: domain.ProtocolTCP,
				Local:    domain.Endpoint{Addr: "127.0.0.1", Port: 5432},
				Remote:   domain.Endpoint{Addr: "127.0.0.1", Port: 55000},
				State:    domain.StateEstablished,
				Inode:    33,
				PID:      200,
			},
			{Protocol: domain.ProtocolTCP, Local: domain.Endpoint{Addr: "127.0.0.1", Port: 3001}, State: domain.StateListen, Inode: 44, PID: 300},
			{Protocol: domain.ProtocolTCP, Local: domain.Endpoint{Addr: "127.0.0.1", Port: 3000}, State: domain.StateListen, Inode: 45, PID: 300},
			{Protocol: domain.ProtocolUDP, Local: domain.Endpoint{Addr: "0.0.0.0", Port: 5353}, State: domain.StateListen, Inode: 55, UID: 991},
		},
	}
}

// manySockets is a snapshot with count listeners on consecutive ports.
func manySockets(count int) domain.Snapshot {
	sockets := make([]domain.Socket, 0, count)
	for index := 0; index < count; index++ {
		sockets = append(sockets, domain.Socket{
			Protocol: domain.ProtocolTCP,
			Local:    domain.Endpoint{Addr: "127.0.0.1", Port: uint16(1000 + index)},
			State:    domain.StateListen,
			Inode:    uint64(index + 1),
			PID:      100,
		})
	}

	return domain.Snapshot{
		Processes: []domain.Process{{PID: 100, Name: "nginx", RSS: mib, StartTicks: 500}},
		Sockets:   sockets,
	}
}

// testModel builds a table of a fixed size with a one-nanosecond ticker, so a
// command never waits for the wall clock.
func testModel(t *testing.T, monitor ports.MonitorService) Model {
	t.Helper()

	m := New(Deps{Monitor: monitor, Keys: keymap.Default(), Interval: time.Nanosecond})
	m.SetBounds(0, 0, 100, 24)

	return m
}

// loadedModel builds a table with the sample snapshot already applied.
func loadedModel(t *testing.T, monitor ports.MonitorService) Model {
	t.Helper()

	return feed(t, testModel(t, monitor), snapshotMsg{seq: 0, snapshot: sampleSnapshot()})
}

// feed applies one message and every command it returns; the ticker is dropped
// so a unit test never depends on the wall clock.
func feed(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()

	if msg == nil {
		return m
	}

	switch msg := msg.(type) {
	case tickMsg:
		return m
	case tea.BatchMsg:
		for _, cmd := range msg {
			if cmd == nil {
				continue
			}

			m = feed(t, m, cmd())
		}

		return m
	}

	next, cmd := m.Update(msg)
	if cmd == nil {
		return next
	}

	return feed(t, next, cmd())
}

// collect runs one command and flattens the messages of a batch.
func collect(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()

	if cmd == nil {
		return nil
	}

	return flatten(t, cmd())
}

// flatten expands a batch into the messages of its commands.
func flatten(t *testing.T, msg tea.Msg) []tea.Msg {
	t.Helper()

	if batch, ok := msg.(tea.BatchMsg); ok {
		msgs := make([]tea.Msg, 0, len(batch))
		for _, cmd := range batch {
			if cmd == nil {
				continue
			}

			msgs = append(msgs, flatten(t, cmd())...)
		}

		return msgs
	}

	return []tea.Msg{msg}
}

func typed(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func key(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

// ansiEscape matches the SGR sequences lipgloss emits.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plain removes the styling of one rendered line.
func plain(line string) string {
	return ansiEscape.ReplaceAllString(line, "")
}

// visibleWidth is the number of cells of one rendered line.
func visibleWidth(line string) int {
	return len([]rune(plain(line)))
}

// checkView asserts the two invariants the root relies on: exactly height lines
// and every line as wide as the panel.
func checkView(t *testing.T, m Model, label string) {
	t.Helper()

	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		t.Fatalf("%s: View() = %d lines, want %d", label, len(lines), m.height)
	}

	for index, line := range lines {
		if got := visibleWidth(line); got != m.width {
			t.Fatalf("%s: line %d has width %d, want %d (%q)", label, index, got, m.width, plain(line))
		}
	}
}

// ports lists the local ports of the table in order.
func portsOf(m Model) []uint16 {
	ports := make([]uint16, 0, len(m.rows))
	for _, item := range m.rows {
		ports = append(ports, item.socket.Local.Port)
	}

	return ports
}

func TestNewDefaults(t *testing.T) {
	m := New(Deps{Keys: keymap.Default()})

	if m.interval != time.Second {
		t.Errorf("interval = %v, want 1s by default", m.interval)
	}
	if got := m.Scope(); got != domain.ScopeAll {
		t.Errorf("Scope() = %v, want all", got)
	}
	if !m.Loading() {
		t.Error("Loading() = false, want the first load pending")
	}
	if m.CapturesInput() {
		t.Error("CapturesInput() = true, want false without the filter")
	}
	if got := m.Counts(); got != (domain.Counts{}) {
		t.Errorf("Counts() = %+v, want the zero value", got)
	}
	if _, _, _, ok := m.Selection(); ok {
		t.Error("Selection() reported a selection on an empty table")
	}
	if m.Err() != nil {
		t.Errorf("Err() = %v, want nil", m.Err())
	}

	lines := strings.Split(m.View(), "\n")
	if len(lines) != defaultHeight {
		t.Fatalf("View() = %d lines, want %d", len(lines), defaultHeight)
	}
	for index, line := range lines {
		if got := visibleWidth(line); got != defaultWidth {
			t.Fatalf("line %d has width %d, want %d", index, got, defaultWidth)
		}
	}
	if !strings.Contains(plain(m.View()), "cargando…") {
		t.Error("View() does not announce the first load")
	}

	short := New(Deps{Interval: time.Millisecond})
	if short.interval != time.Millisecond {
		t.Errorf("interval = %v, want the requested one", short.interval)
	}
	if len(short.keys.All()) == 0 {
		t.Error("an empty keymap did not fall back to the defaults")
	}
}

func TestSnapshotAppliesTheScope(t *testing.T) {
	m := loadedModel(t, newFakeMonitor(sampleSnapshot()))

	if got := len(m.rows); got != 6 {
		t.Fatalf("rows = %d, want 6", got)
	}

	m.SetScope(domain.ScopeListen)
	if got := len(m.rows); got != 5 {
		t.Fatalf("listen rows = %d, want 5", got)
	}
	for _, item := range m.rows {
		if !item.socket.IsListening() {
			t.Errorf("listen scope kept %v", item.socket.State)
		}
	}

	m.SetScope(domain.ScopeConnected)
	if got := len(m.rows); got != 1 {
		t.Fatalf("connected rows = %d, want 1", got)
	}
	if got := m.rows[0].socket.Inode; got != 33 {
		t.Errorf("connected row inode = %d, want 33", got)
	}

	m.SetScope(domain.ScopeAll)
	if got := len(m.rows); got != 6 {
		t.Fatalf("all rows = %d, want 6", got)
	}
}

func TestEndpointKeepsThePortWhenTruncated(t *testing.T) {
	t.Parallel()

	if got, want := truncateLeft("127.0.0.1:34567", 13), "….0.0.1:34567"; got != want {
		t.Errorf("truncateLeft() = %q, want %q", got, want)
	}
	if got, want := truncateLeft("*:8080", 13), "*:8080"; got != want {
		t.Errorf("truncateLeft() = %q, want the untouched endpoint", got)
	}
}

func TestRowsAreOrderedByPort(t *testing.T) {
	m := loadedModel(t, newFakeMonitor(sampleSnapshot()))

	want := []uint16{80, 3000, 3001, 5353, 5432, 5432}
	if got := portsOf(m); !equalPorts(got, want) {
		t.Fatalf("ports = %v, want %v", got, want)
	}

	if got := m.rows[0].socket.Inode; got != 11 {
		t.Errorf("first row inode = %d, want the nginx listener", got)
	}

	view := plain(m.View())
	for _, want := range []string{
		"PROTO", "ENDPOINT", "ESTADO", "PROCESO", "RSS",
		"puertos", "todas", "6 puertos", "orden: puerto",
		"*:80", "listen", "nginx", "12.0 MiB", "established", "?",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("View() does not contain %q", want)
		}
	}

	orphans := make([]row, 0, 1)
	for _, item := range m.rows {
		if !item.socket.Attributed() {
			orphans = append(orphans, item)
		}
	}
	if len(orphans) != 1 {
		t.Fatalf("rows without an owner = %d, want 1", len(orphans))
	}
	if !strings.Contains(orphans[0].text, "?") {
		t.Errorf("unattributed row = %q, want the unknown process", orphans[0].text)
	}
	if !strings.HasSuffix(orphans[0].text, "-") {
		t.Errorf("unattributed row = %q, want the RSS placeholder", orphans[0].text)
	}
}

func equalPorts(got, want []uint16) bool {
	if len(got) != len(want) {
		return false
	}

	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}

	return true
}

func TestSelectionIsCachedAndKeepsTheIDAcrossRefreshes(t *testing.T) {
	monitor := newFakeMonitor(sampleSnapshot())
	m := loadedModel(t, monitor)

	m = feed(t, m, key(tea.KeyDown))
	socket, process, siblings, ok := m.Selection()
	if !ok {
		t.Fatal("Selection() reported no selection")
	}
	// the second row is node on port 3000
	if socket.Inode != 45 {
		t.Fatalf("selected inode = %d, want 45", socket.Inode)
	}
	if process.Name != "node" {
		t.Fatalf("selected process = %q, want node", process.Name)
	}
	if len(siblings) != 2 || siblings[0].Local.Port != 3000 || siblings[1].Local.Port != 3001 {
		t.Fatalf("siblings = %+v, want the two sockets of node ordered by port", siblings)
	}

	refreshed := sampleSnapshot()
	refreshed.Processes = append(refreshed.Processes, domain.Process{PID: 400, Name: "sshd", RSS: 4 * mib})
	refreshed.Sockets = append([]domain.Socket{{
		Protocol: domain.ProtocolTCP,
		Local:    domain.Endpoint{Addr: "127.0.0.1", Port: 22},
		State:    domain.StateListen,
		Inode:    66,
		PID:      400,
	}}, refreshed.Sockets...)
	monitor.setSnapshot(refreshed)

	m = feed(t, m, typed('r'))

	if got := len(m.rows); got != 7 {
		t.Fatalf("rows = %d, want 7 after the refresh", got)
	}
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want the refreshed position of the same socket", m.cursor)
	}

	socket, process, _, ok = m.Selection()
	if !ok || socket.Inode != 45 {
		t.Fatalf("Selection() = %+v, want the socket selected before the refresh", socket)
	}
	if process.Name != "node" {
		t.Errorf("selected process = %q, want node after the refresh", process.Name)
	}
}

func TestSetScopeKeepsTheSelectedSocket(t *testing.T) {
	m := loadedModel(t, newFakeMonitor(sampleSnapshot()))

	m = feed(t, m, key(tea.KeyEnd))
	if socket, _, _, ok := m.Selection(); !ok || socket.Inode != 33 {
		t.Fatalf("Selection() = %+v, want the established socket", socket)
	}

	m.SetScope(domain.ScopeConnected)

	socket, _, _, ok := m.Selection()
	if !ok || socket.Inode != 33 {
		t.Fatalf("Selection() after the scope change = %+v, want inode 33", socket)
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}

	m.SetScope(domain.ScopeAll)

	socket, _, _, _ = m.Selection()
	if socket.Inode != 33 {
		t.Errorf("Selection() after widening = %+v, want inode 33", socket)
	}
	if got := m.Scope(); got != domain.ScopeAll {
		t.Errorf("Scope() = %v, want all", got)
	}
	if got := plain(m.View()); !strings.Contains(got, "todas") {
		t.Errorf("View() = %q, want the scope label of every socket", got)
	}
}

func TestSortCyclesTheOrder(t *testing.T) {
	m := loadedModel(t, newFakeMonitor(sampleSnapshot()))

	m = feed(t, m, typed('s'))
	if got := plain(m.View()); !strings.Contains(got, "orden: rss") {
		t.Fatalf("View() = %q, want the rss order", got)
	}
	if got := m.rows[0].socket.PID; got != 200 {
		t.Errorf("first row pid = %d, want the biggest RSS", got)
	}
	if got := m.rows[len(m.rows)-1].socket.PID; got != 0 {
		t.Errorf("last row pid = %d, want the socket without an owner", got)
	}

	m = feed(t, m, typed('s'))
	if got := plain(m.View()); !strings.Contains(got, "orden: proceso") {
		t.Fatalf("View() = %q, want the process order", got)
	}
	if got := m.rows[0].name(); got != "?" {
		t.Errorf("first name = %q, want the unknown process first", got)
	}
	if got := m.rows[1].name(); got != "nginx" {
		t.Errorf("second name = %q, want nginx", got)
	}

	m = feed(t, m, typed('s'))
	if got := plain(m.View()); !strings.Contains(got, "orden: pid") {
		t.Fatalf("View() = %q, want the pid order", got)
	}
	wantPIDs := []int{0, 100, 200, 200, 300, 300}
	for index, want := range wantPIDs {
		if got := m.rows[index].socket.PID; got != want {
			t.Fatalf("row %d pid = %d, want %d", index, got, want)
		}
	}

	m = feed(t, m, typed('s'))
	if got := plain(m.View()); !strings.Contains(got, "orden: puerto") {
		t.Fatalf("View() = %q, want the port order back", got)
	}
	if got := portsOf(m); !equalPorts(got, []uint16{80, 3000, 3001, 5353, 5432, 5432}) {
		t.Errorf("ports = %v, want the port order", got)
	}
}

func TestFilterNarrowsAndEscapeClears(t *testing.T) {
	m := loadedModel(t, newFakeMonitor(sampleSnapshot()))

	m = feed(t, m, typed('/'))
	if !m.CapturesInput() {
		t.Fatal("CapturesInput() = false, want the filter to own the keyboard")
	}

	for _, r := range "postgres" {
		m = feed(t, m, typed(r))
	}

	if got := len(m.rows); got != 2 {
		t.Fatalf("rows = %d, want the two sockets of postgres", got)
	}
	if !strings.Contains(plain(m.View()), "filtro") {
		t.Error("View() does not show the filter line")
	}

	m = feed(t, m, key(tea.KeyEnter))

	if m.CapturesInput() {
		t.Error("enter left the filter open")
	}
	if m.filter != "postgres" {
		t.Errorf("filter = %q, want the text kept after enter", m.filter)
	}
	if got := len(m.rows); got != 2 {
		t.Errorf("rows = %d, want the filter kept after enter", got)
	}

	m = feed(t, m, key(tea.KeyEscape))

	if m.filter != "" {
		t.Errorf("filter = %q, want it cleared by esc", m.filter)
	}
	if m.CapturesInput() {
		t.Error("esc left the filter open")
	}
	if got := len(m.rows); got != 6 {
		t.Errorf("rows = %d, want every row back", got)
	}
}

func TestFilterMatchesEveryField(t *testing.T) {
	m := loadedModel(t, newFakeMonitor(sampleSnapshot()))

	m = feed(t, m, typed('/'))
	for _, r := range "5432" {
		m = feed(t, m, typed(r))
	}
	if got := len(m.rows); got != 2 {
		t.Fatalf("rows = %d, want the two sockets on port 5432", got)
	}

	m = feed(t, m, key(tea.KeyEscape))
	m = feed(t, m, typed('/'))
	for _, r := range "www-data" {
		m = feed(t, m, typed(r))
	}
	if got := len(m.rows); got != 1 || m.rows[0].socket.PID != 100 {
		t.Fatalf("rows = %d, want the socket of the www-data user", len(m.rows))
	}

	m = feed(t, m, key(tea.KeyEscape))
	m = feed(t, m, typed('/'))
	for _, r := range "UDP" {
		m = feed(t, m, typed(r))
	}
	if got := len(m.rows); got != 1 {
		t.Fatalf("rows = %d, want the only udp socket", len(m.rows))
	}
	if got := m.rows[0].socket.Protocol; got != domain.ProtocolUDP {
		t.Errorf("protocol = %v, want udp", got)
	}

	// A socket without an owner is still findable by its numeric uid.
	m = feed(t, m, key(tea.KeyEscape))
	m = feed(t, m, typed('/'))
	for _, r := range "991" {
		m = feed(t, m, typed(r))
	}
	if got := len(m.rows); got != 1 || m.rows[0].socket.UID != 991 {
		t.Fatalf("rows = %d, want the socket with uid 991", got)
	}

	m = feed(t, m, key(tea.KeyEscape))
	m = feed(t, m, typed('/'))
	for _, r := range "no-existe" {
		m = feed(t, m, typed(r))
	}

	if got := plain(m.View()); !strings.Contains(got, "sin coincidencias para el filtro") {
		t.Errorf("View() = %q, want the empty state of the filter", got)
	}
}

func TestCountsSummariseTheWholeSnapshot(t *testing.T) {
	m := loadedModel(t, newFakeMonitor(sampleSnapshot()))

	want := domain.Counts{
		Ports:     6,
		Listening: 5,
		Connected: 1,
		Processes: 3,
		RSS:       12*mib + 256*mib + 90*kib,
	}
	if got := m.Counts(); got != want {
		t.Fatalf("Counts() = %+v, want %+v", got, want)
	}

	m.SetScope(domain.ScopeConnected)
	m = feed(t, m, typed('/'))
	m = feed(t, m, typed('n'))

	if got := m.Counts(); got != want {
		t.Errorf("Counts() = %+v, want the totals of the snapshot", got)
	}
}

func TestStateLinesExplainEveryEmptyTable(t *testing.T) {
	unavailable := New(Deps{Keys: keymap.Default(), Interval: time.Nanosecond})
	unavailable.SetBounds(0, 0, 100, 24)
	unavailable = feed(t, unavailable, snapshotMsg{seq: 0, err: domain.ErrUnavailable})

	if !errors.Is(unavailable.Err(), domain.ErrUnavailable) {
		t.Fatalf("Err() = %v, want ErrUnavailable", unavailable.Err())
	}
	view := plain(unavailable.View())
	if !strings.Contains(view, "monitor no disponible") || !strings.Contains(view, "reintentar") {
		t.Errorf("View() = %q, want the actionable unavailable message", view)
	}
	if !strings.Contains(view, "r") {
		t.Errorf("View() = %q, want the key that retries", view)
	}

	unsupported := feed(t, testModel(t, nil), snapshotMsg{seq: 0, err: domain.ErrUnsupported})
	if got := plain(unsupported.View()); !strings.Contains(got, "sistema no soportado") {
		t.Errorf("View() = %q, want the unsupported message", got)
	}

	empty := feed(t, testModel(t, newFakeMonitor(domain.Snapshot{})), snapshotMsg{seq: 0, snapshot: domain.Snapshot{}})
	if empty.Loading() {
		t.Error("Loading() = true after an empty snapshot")
	}
	if got := plain(empty.View()); !strings.Contains(got, "sin puertos en esta pestaña") {
		t.Errorf("View() = %q, want the empty state", got)
	}

	m := loadedModel(t, newFakeMonitor(sampleSnapshot()))
	m.SetScope(domain.ScopeConnected)
	m = feed(t, m, typed('/'))
	for _, r := range "nada-que-ver" {
		m = feed(t, m, typed(r))
	}
	if got := plain(m.View()); !strings.Contains(got, "sin coincidencias para el filtro") {
		t.Errorf("View() = %q, want the filter state", got)
	}
}

func TestClickSelectsTheRowUnderThePointer(t *testing.T) {
	m := loadedModel(t, newFakeMonitor(sampleSnapshot()))
	m.SetBounds(4, 2, 100, 24)

	// The body starts two lines below the top of the panel: y = 2 + 2 = 4.
	m = feed(t, m, tea.MouseClickMsg{X: 6, Y: 5, Button: tea.MouseLeft})

	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want the clicked row", m.cursor)
	}
	if socket, _, _, ok := m.Selection(); !ok || socket.Inode != 45 {
		t.Fatalf("Selection() = %+v, want the clicked socket", socket)
	}

	// A click on the header and a click outside the panel move nothing.
	for _, point := range []tea.MouseClickMsg{
		{X: 6, Y: 3, Button: tea.MouseLeft},
		{X: 200, Y: 40, Button: tea.MouseLeft},
		{X: 6, Y: 5, Button: tea.MouseRight},
	} {
		if next, _ := m.Update(point); next.cursor != 1 {
			t.Errorf("click %+v moved the cursor to %d", point, next.cursor)
		}
	}

	// The filter line pushes the body one row down.
	m = feed(t, m, typed('/'))
	m = feed(t, m, tea.MouseClickMsg{X: 6, Y: 5, Button: tea.MouseLeft})
	if socket, _, _, _ := m.Selection(); socket.Inode != 11 {
		t.Errorf("Selection() = %+v, want the first row under the filter line", socket)
	}
}

func TestWheelScrollsTheWindow(t *testing.T) {
	m := testModel(t, newFakeMonitor(manySockets(30)))
	m.SetBounds(0, 0, 100, 10)
	m = feed(t, m, snapshotMsg{seq: 0, snapshot: manySockets(30)})

	if got := m.bodyHeight(); got != 7 {
		t.Fatalf("bodyHeight() = %d, want 7", got)
	}

	m = feed(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if m.offset != wheelStep {
		t.Fatalf("offset = %d, want %d", m.offset, wheelStep)
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want the wheel not to move the selection", m.cursor)
	}

	m = feed(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if m.offset != 0 {
		t.Errorf("offset = %d, want it back at the top", m.offset)
	}

	for range 20 {
		m = feed(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	}
	if want := len(m.rows) - m.bodyHeight(); m.offset != want {
		t.Errorf("offset = %d, want it clamped to %d", m.offset, want)
	}

	view := plain(m.View())
	if strings.Contains(view, "127.0.0.1:1000") {
		t.Error("View() rendered a row above the window")
	}
	if !strings.Contains(view, "127.0.0.1:1029") {
		t.Error("View() does not render the last row")
	}
}

func TestViewKeepsTheLineAndWidthInvariants(t *testing.T) {
	sizes := []struct{ width, height int }{
		{40, 10}, {80, 10}, {120, 10},
		{40, 24}, {80, 24}, {120, 24},
	}

	for _, size := range sizes {
		loaded := testModel(t, newFakeMonitor(sampleSnapshot()))
		loaded.SetBounds(0, 0, size.width, size.height)
		loaded = feed(t, loaded, snapshotMsg{seq: 0, snapshot: sampleSnapshot()})
		checkView(t, loaded, fmt.Sprintf("loaded %dx%d", size.width, size.height))

		if size.width >= 80 && !strings.Contains(plain(loaded.View()), "256.0 MiB") {
			t.Errorf("%dx%d: the RSS of the biggest process is not visible", size.width, size.height)
		}

		empty := testModel(t, newFakeMonitor(domain.Snapshot{}))
		empty.SetBounds(0, 0, size.width, size.height)
		empty = feed(t, empty, snapshotMsg{seq: 0, snapshot: domain.Snapshot{}})
		checkView(t, empty, fmt.Sprintf("empty %dx%d", size.width, size.height))

		filtering := loaded
		filtering = feed(t, filtering, typed('/'))
		for _, r := range "no-existe" {
			filtering = feed(t, filtering, typed(r))
		}
		checkView(t, filtering, fmt.Sprintf("filtering %dx%d", size.width, size.height))

		filtered := filtering
		filtered = feed(t, filtered, key(tea.KeyEnter))
		checkView(t, filtered, fmt.Sprintf("filtered %dx%d", size.width, size.height))
	}

	tiny := testModel(t, newFakeMonitor(sampleSnapshot()))
	tiny.SetBounds(0, 0, 12, 2)
	checkView(t, tiny, "tiny")
}

func TestViewPadsEveryRowToTheWidth(t *testing.T) {
	m := loadedModel(t, newFakeMonitor(sampleSnapshot()))
	m.SetBounds(0, 0, 60, 12)

	lines := strings.Split(m.View(), "\n")
	for index, line := range lines {
		if got := visibleWidth(line); got != 60 {
			t.Fatalf("line %d has width %d, want 60", index, got)
		}
	}

	header := plain(lines[1])
	if !strings.HasPrefix(header, "PROTO") {
		t.Errorf("column header = %q, want the protocol column first", header)
	}
	if !strings.HasSuffix(strings.TrimRight(header, " "), "RSS") {
		t.Errorf("column header = %q, want the RSS column last", header)
	}
}

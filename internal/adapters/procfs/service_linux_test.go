//go:build linux

package procfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MiguelFPP/tui-port/internal/domain"
)

const (
	// btimeSeconds is the boot time of the fake mount, in Unix seconds.
	btimeSeconds = 1700000000
	// pidOwner and pidListener are the two processes of the fake mount.
	pidOwner    = 4242
	pidListener = 4343
	// uidOwner exists on every Linux host and uidMissing does not.
	uidOwner   = 0
	uidMissing = 4000000001
)

// buildFixture writes a fake proc mount with two owners and one unattributed socket.
func buildFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	writeFixture(t, filepath.Join(root, "stat"),
		"cpu  1 2 3 4\nbtime "+strconv.Itoa(btimeSeconds)+"\nprocesses 900\n")

	writeFixture(t, filepath.Join(root, "net", "tcp"), netHeader+
		netRow("00000000:1F90", "00000000:0000", "0A", uidOwner, 300001)+
		netRow("0100007F:C350", "0100007F:1F91", "01", uidOwner, 300002)+
		netRow("00000000:2328", "00000000:0000", "0A", uidMissing, 300003)+
		"   esta fila esta rota\n")

	writeFixture(t, filepath.Join(root, "net", "tcp6"), netHeader+
		netRow("00000000000000000000000001000000:0050", "00000000000000000000000000000000:0000", "0A", uidOwner, 300004))

	writeFixture(t, filepath.Join(root, "net", "udp"), netHeader+
		netRow("00000000:14E9", "00000000:0000", "07", uidOwner, 300005))

	writeFixture(t, filepath.Join(root, "net", "udp6"), netHeader)

	ownerDir := filepath.Join(root, strconv.Itoa(pidOwner))
	writeFixture(t, filepath.Join(ownerDir, "status"), statusFile(pidOwner, "odd ) name", 1, uidOwner, 12864, 4))
	writeFixture(t, filepath.Join(ownerDir, "cmdline"), "sleeper\x00--daemon\x00")
	writeFixture(t, filepath.Join(ownerDir, "stat"), statFile(pidOwner, "odd ) name", 500))
	linkFixture(t, filepath.Join(ownerDir, "fd", "3"), "socket:[300001]")
	linkFixture(t, filepath.Join(ownerDir, "fd", "4"), "/dev/null")
	linkFixture(t, filepath.Join(ownerDir, "fd", "5"), "pipe:[42]")
	linkFixture(t, filepath.Join(ownerDir, "fd", "9"), "socket:[300004]")

	listenerDir := filepath.Join(root, strconv.Itoa(pidListener))
	writeFixture(t, filepath.Join(listenerDir, "status"), statusFile(pidListener, "listener", pidOwner, uidMissing, 2048, 2))
	writeFixture(t, filepath.Join(listenerDir, "cmdline"), "")
	linkFixture(t, filepath.Join(listenerDir, "fd", "3"), "socket:[300002]")
	linkFixture(t, filepath.Join(listenerDir, "fd", "4"), "socket:[300005]")

	return root
}

func TestNewUsesProcWhenRootIsEmpty(t *testing.T) {
	service, err := New("")
	if err != nil {
		t.Fatalf("New(\"\") error = %v, want nil", err)
	}
	if service.root != defaultRoot {
		t.Errorf("root = %q, want %q", service.root, defaultRoot)
	}

	spaced, err := New("   ")
	if err != nil {
		t.Fatalf("New(\"   \") error = %v, want nil", err)
	}
	if spaced.root != defaultRoot {
		t.Errorf("root = %q, want %q", spaced.root, defaultRoot)
	}
}

func TestNewRejectsBadRoots(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "procfs-no-existe")

	service, err := New(missing)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("New(%q) error = %v, want %v", missing, err, domain.ErrNotFound)
	}
	if service != nil {
		t.Errorf("New(%q) service = %v, want nil", missing, service)
	}

	file := filepath.Join(t.TempDir(), "stat")
	writeFixture(t, file, "btime 1700000000\n")

	if _, err := New(file); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("New(%q) error = %v, want %v", file, err, domain.ErrInvalidArgument)
	}
}

func TestSnapshotReadsFixture(t *testing.T) {
	service, err := New(buildFixture(t))
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if snapshot.Truncated {
		t.Error("Truncated = true, want false")
	}
	if snapshot.TakenAt.IsZero() {
		t.Error("TakenAt = zero, want la hora de la lectura")
	}

	wantInodes := []uint64{300001, 300002, 300003, 300004, 300005}
	wantProtocols := []domain.Protocol{
		domain.ProtocolTCP, domain.ProtocolTCP, domain.ProtocolTCP,
		domain.ProtocolTCP6, domain.ProtocolUDP,
	}
	wantStates := []domain.SocketState{
		domain.StateListen, domain.StateEstablished, domain.StateListen,
		domain.StateListen, domain.StateListen,
	}
	wantPIDs := []int{pidOwner, pidListener, 0, pidOwner, pidListener}

	if len(snapshot.Sockets) != len(wantInodes) {
		t.Fatalf("Sockets = %d filas, want %d", len(snapshot.Sockets), len(wantInodes))
	}

	for index, socket := range snapshot.Sockets {
		if socket.Inode != wantInodes[index] {
			t.Errorf("Sockets[%d].Inode = %d, want %d", index, socket.Inode, wantInodes[index])
		}
		if socket.Protocol != wantProtocols[index] {
			t.Errorf("Sockets[%d].Protocol = %v, want %v", index, socket.Protocol, wantProtocols[index])
		}
		if socket.State != wantStates[index] {
			t.Errorf("Sockets[%d].State = %v, want %v", index, socket.State, wantStates[index])
		}
		if socket.PID != wantPIDs[index] {
			t.Errorf("Sockets[%d].PID = %d, want %d", index, socket.PID, wantPIDs[index])
		}
	}

	if got, want := snapshot.Sockets[0].Local, (domain.Endpoint{Addr: "0.0.0.0", Port: 8080}); got != want {
		t.Errorf("Sockets[0].Local = %v, want %v", got, want)
	}
	if got, want := snapshot.Sockets[1].Remote, (domain.Endpoint{Addr: "127.0.0.1", Port: 8081}); got != want {
		t.Errorf("Sockets[1].Remote = %v, want %v", got, want)
	}
	if got, want := snapshot.Sockets[3].Local, (domain.Endpoint{Addr: "::1", Port: 80}); got != want {
		t.Errorf("Sockets[3].Local = %v, want %v", got, want)
	}

	if snapshot.Sockets[2].Attributed() {
		t.Error("el socket sin dueno quedo atribuido, want PID 0")
	}
	if snapshot.Sockets[2].UID != uidMissing {
		t.Errorf("Sockets[2].UID = %d, want %d", snapshot.Sockets[2].UID, uidMissing)
	}

	if len(snapshot.Processes) != 2 {
		t.Fatalf("Processes = %d, want 2", len(snapshot.Processes))
	}
	if snapshot.Processes[0].PID != pidOwner || snapshot.Processes[1].PID != pidListener {
		t.Errorf("PIDs = %d, %d, want %d, %d",
			snapshot.Processes[0].PID, snapshot.Processes[1].PID, pidOwner, pidListener)
	}

	owner, ok := snapshot.Process(pidOwner)
	if !ok {
		t.Fatalf("Process(%d) no encontrado", pidOwner)
	}

	if owner.Name != "odd ) name" {
		t.Errorf("Name = %q, want %q", owner.Name, "odd ) name")
	}
	if owner.PPID != 1 {
		t.Errorf("PPID = %d, want 1", owner.PPID)
	}
	if owner.Cmdline != "sleeper --daemon" {
		t.Errorf("Cmdline = %q, want %q", owner.Cmdline, "sleeper --daemon")
	}
	if owner.User != "root" && owner.User != "0" {
		t.Errorf("User = %q, want %q o %q", owner.User, "root", "0")
	}
	if owner.RSS != 12864*1024 {
		t.Errorf("RSS = %d, want %d", owner.RSS, 12864*1024)
	}
	if owner.Threads != 4 {
		t.Errorf("Threads = %d, want 4", owner.Threads)
	}
	if want := time.Unix(btimeSeconds+5, 0).UTC(); !owner.StartedAt.Equal(want) {
		t.Errorf("StartedAt = %v, want %v", owner.StartedAt, want)
	}

	listener, ok := snapshot.Process(pidListener)
	if !ok {
		t.Fatalf("Process(%d) no encontrado", pidListener)
	}

	if listener.Name != "listener" {
		t.Errorf("Name = %q, want %q", listener.Name, "listener")
	}
	if listener.PPID != pidOwner {
		t.Errorf("PPID = %d, want %d", listener.PPID, pidOwner)
	}
	if listener.Cmdline != "" {
		t.Errorf("Cmdline = %q, want vacia", listener.Cmdline)
	}
	if want := strconv.FormatUint(uidMissing, 10); listener.User != want {
		t.Errorf("User = %q, want el UID numerico %q", listener.User, want)
	}
	if listener.RSS != 2048*1024 {
		t.Errorf("RSS = %d, want %d", listener.RSS, 2048*1024)
	}
	if listener.Threads != 2 {
		t.Errorf("Threads = %d, want 2", listener.Threads)
	}
	if !listener.StartedAt.IsZero() {
		t.Errorf("StartedAt = %v, want el valor cero sin <pid>/stat", listener.StartedAt)
	}

	owned := snapshot.SocketsOf(pidOwner)
	if len(owned) != 2 {
		t.Fatalf("SocketsOf(%d) = %d sockets, want 2", pidOwner, len(owned))
	}
	if owned[0].Inode != 300001 || owned[1].Inode != 300004 {
		t.Errorf("SocketsOf(%d) = %d, %d, want 300001, 300004", pidOwner, owned[0].Inode, owned[1].Inode)
	}

	if got := snapshot.SocketsOf(0); got != nil {
		t.Errorf("SocketsOf(0) = %v, want nil", got)
	}
	if got := snapshot.SocketsOf(-1); got != nil {
		t.Errorf("SocketsOf(-1) = %v, want nil", got)
	}

	counts := snapshot.Counts()
	if counts.Ports != 5 {
		t.Errorf("Counts().Ports = %d, want 5", counts.Ports)
	}
	if counts.Listening != 4 {
		t.Errorf("Counts().Listening = %d, want 4", counts.Listening)
	}
	if counts.Connected != 1 {
		t.Errorf("Counts().Connected = %d, want 1", counts.Connected)
	}
	if counts.Processes != 2 {
		t.Errorf("Counts().Processes = %d, want 2", counts.Processes)
	}
	if want := uint64(12864+2048) * 1024; counts.RSS != want {
		t.Errorf("Counts().RSS = %d, want %d", counts.RSS, want)
	}
}

func TestSnapshotToleratesMissingTables(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "net", "tcp"),
		netHeader+netRow("00000000:0016", "00000000:0000", "0A", uidOwner, 77))

	service, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Sockets) != 1 {
		t.Fatalf("Sockets = %d filas, want 1", len(snapshot.Sockets))
	}
	if snapshot.Sockets[0].State != domain.StateListen {
		t.Errorf("State = %v, want %v", snapshot.Sockets[0].State, domain.StateListen)
	}
	if snapshot.Sockets[0].PID != 0 {
		t.Errorf("PID = %d, want 0 sin dueno", snapshot.Sockets[0].PID)
	}
	if len(snapshot.Processes) != 0 {
		t.Errorf("Processes = %d filas, want 0", len(snapshot.Processes))
	}
	if snapshot.Truncated {
		t.Error("Truncated = true, want false")
	}
}

func TestSnapshotWithoutTables(t *testing.T) {
	service, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	if _, err := service.Snapshot(context.Background()); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("Snapshot() error = %v, want %v", err, domain.ErrUnavailable)
	}
}

func TestSnapshotBoundsSockets(t *testing.T) {
	var builder strings.Builder
	builder.WriteString(netHeader)
	for index := 0; index <= domain.MaxSockets; index++ {
		builder.WriteString(netRow("00000000:1F90", "00000000:0000", "0A", uidOwner, uint64(300000+index)))
	}

	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "net", "tcp"), builder.String())

	service, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Sockets) != domain.MaxSockets {
		t.Errorf("Sockets = %d filas, want %d", len(snapshot.Sockets), domain.MaxSockets)
	}
	if !snapshot.Truncated {
		t.Error("Truncated = false, want true con el tope alcanzado")
	}
}

func TestSnapshotRejectsCanceledContext(t *testing.T) {
	service, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.Snapshot(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Snapshot() error = %v, want %v", err, context.Canceled)
	}
}

func TestKillRejectsInvalidArguments(t *testing.T) {
	service, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	for _, pid := range []int{0, -1, -4242} {
		if err := service.Kill(context.Background(), pid, domain.SignalTerm); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Errorf("Kill(%d) error = %v, want %v", pid, err, domain.ErrInvalidArgument)
		}
	}

	if err := service.Kill(context.Background(), 4000000, domain.Signal(9)); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("Kill() con una senal desconocida error = %v, want %v", err, domain.ErrInvalidArgument)
	}
}

func TestKillProtectsCriticalProcesses(t *testing.T) {
	service, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	for _, pid := range []int{1, os.Getpid()} {
		if err := service.Kill(context.Background(), pid, domain.SignalKill); !errors.Is(err, domain.ErrProcessProtected) {
			t.Errorf("Kill(%d) error = %v, want %v", pid, err, domain.ErrProcessProtected)
		}
	}
}

func TestKillRejectsCanceledContext(t *testing.T) {
	service, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Kill(ctx, 4000000, domain.SignalTerm); !errors.Is(err, context.Canceled) {
		t.Errorf("Kill() error = %v, want %v", err, context.Canceled)
	}
}

func TestSignalErrorTranslates(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "sin proceso", err: fmt.Errorf("kill: %w", syscall.ESRCH), want: domain.ErrNotFound},
		{name: "sin permiso", err: syscall.EPERM, want: domain.ErrPermissionDenied},
		{name: "acceso denegado", err: fmt.Errorf("kill: %w", syscall.EACCES), want: domain.ErrPermissionDenied},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := signalError(4242, test.err); !errors.Is(err, test.want) {
				t.Errorf("signalError() = %v, want %v", err, test.want)
			}
		})
	}

	err := signalError(4242, errors.New("kernel roto"))
	if err == nil {
		t.Fatal("signalError() = nil, want un error tecnico envuelto")
	}
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrPermissionDenied) {
		t.Errorf("signalError() = %v, want un error tecnico sin sentinelas", err)
	}
}

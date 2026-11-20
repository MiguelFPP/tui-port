package domain

import "testing"

func TestSnapshotProcess(t *testing.T) {
	t.Parallel()

	snapshot := Snapshot{Processes: []Process{{PID: 1, Name: "init"}, {PID: 42, Name: "node"}}}

	process, ok := snapshot.Process(42)
	if !ok || process.Name != "node" {
		t.Fatalf("Process(42) = %+v, %v", process, ok)
	}
	if _, ok := snapshot.Process(7); ok {
		t.Error("Process(7) must not be found")
	}
}

func TestSnapshotSocketsOf(t *testing.T) {
	t.Parallel()

	snapshot := Snapshot{Sockets: []Socket{
		{PID: 42, Local: Endpoint{Port: 3000}},
		{PID: 7, Local: Endpoint{Port: 8000}},
		{PID: 42, Local: Endpoint{Port: 8080}},
		{PID: 0, Local: Endpoint{Port: 5432}},
	}}

	sockets := snapshot.SocketsOf(42)
	if len(sockets) != 2 {
		t.Fatalf("SocketsOf(42) returned %d sockets, want 2", len(sockets))
	}
	if sockets[0].Local.Port != 3000 || sockets[1].Local.Port != 8080 {
		t.Errorf("SocketsOf(42) = %+v", sockets)
	}
	if got := snapshot.SocketsOf(0); got != nil {
		t.Errorf("SocketsOf(0) = %+v, want nil", got)
	}
}

func TestSnapshotCounts(t *testing.T) {
	t.Parallel()

	snapshot := Snapshot{
		Sockets: []Socket{
			{State: StateListen},
			{State: StateListen},
			{State: StateEstablished, Remote: Endpoint{Addr: "10.0.0.1", Port: 443}},
			{State: StateTimeWait},
			{State: StateEstablished, Remote: Endpoint{Addr: "10.0.0.2", Port: 80}},
		},
		Processes: []Process{{PID: 1, RSS: 1024}, {PID: 2, RSS: 2048}},
	}

	counts := snapshot.Counts()
	if counts.Ports != 5 || counts.Listening != 2 || counts.Connected != 2 {
		t.Errorf("Counts() = %+v", counts)
	}
	if counts.Processes != 2 || counts.RSS != 3072 {
		t.Errorf("Counts() = %+v", counts)
	}
}

func TestProcessLabel(t *testing.T) {
	t.Parallel()

	if got := (Process{Name: "node"}).Label(); got != "node" {
		t.Errorf("Label() = %q", got)
	}
	if got := (Process{PID: 9}).Label(); got != "pid 9" {
		t.Errorf("Label() = %q", got)
	}
}

func TestSignalString(t *testing.T) {
	t.Parallel()

	if got := SignalKill.String(); got != "KILL" {
		t.Errorf("SignalKill.String() = %q", got)
	}
	if got := SignalTerm.String(); got != "TERM" {
		t.Errorf("SignalTerm.String() = %q", got)
	}
}

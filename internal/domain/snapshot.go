package domain

import "time"

// Snapshot is one coherent reading of the sockets and the processes that own
// them. Processes without sockets are not included.
type Snapshot struct {
	TakenAt   time.Time
	Sockets   []Socket
	Processes []Process
	Truncated bool
}

// Process returns the process with the given PID.
func (s Snapshot) Process(pid int) (Process, bool) {
	for _, process := range s.Processes {
		if process.PID == pid {
			return process, true
		}
	}

	return Process{}, false
}

// SocketsOf returns every socket owned by pid, in snapshot order.
func (s Snapshot) SocketsOf(pid int) []Socket {
	if pid <= 0 {
		return nil
	}

	sockets := make([]Socket, 0, 4)
	for _, socket := range s.Sockets {
		if socket.PID == pid {
			sockets = append(sockets, socket)
		}
	}

	return sockets
}

// Counts are the totals the status bar shows.
type Counts struct {
	Ports     int
	Listening int
	Connected int
	Processes int
	RSS       uint64
}

// Counts summarises the whole snapshot, regardless of the active scope.
func (s Snapshot) Counts() Counts {
	counts := Counts{
		Ports:     len(s.Sockets),
		Processes: len(s.Processes),
	}

	for _, socket := range s.Sockets {
		switch {
		case socket.IsListening():
			counts.Listening++
		case socket.HasPeer():
			counts.Connected++
		}
	}

	for _, process := range s.Processes {
		counts.RSS += process.RSS
	}

	return counts
}

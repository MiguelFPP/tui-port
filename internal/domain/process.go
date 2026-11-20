package domain

import (
	"strconv"
	"time"
)

// Process is one process that owns at least one socket of the snapshot.
type Process struct {
	PID       int
	PPID      int
	Name      string
	Cmdline   string
	User      string
	RSS       uint64
	Threads   int
	StartedAt time.Time

	// StartTicks is the kernel start time in clock ticks since boot. The PID
	// alone is not a stable identity because the kernel recycles it; this value
	// is checked again before a signal is sent.
	StartTicks uint64
}

// ProcessRef identifies one process instance from a snapshot. A PID is not
// enough: the kernel can reuse it after the original process exits.
type ProcessRef struct {
	PID        int
	StartTicks uint64
}

// Ref returns the identity of the process instance.
func (p Process) Ref() ProcessRef {
	return ProcessRef{PID: p.PID, StartTicks: p.StartTicks}
}

// Label names the process, falling back to its PID.
func (p Process) Label() string {
	if p.Name != "" {
		return p.Name
	}

	return "pid " + strconv.Itoa(p.PID)
}

// Signal is the termination request the monitor can send.
type Signal uint8

const (
	SignalKill Signal = iota
	SignalTerm
)

func (s Signal) String() string {
	if s == SignalTerm {
		return "TERM"
	}

	return "KILL"
}

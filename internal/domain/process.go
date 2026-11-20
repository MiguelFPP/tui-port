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

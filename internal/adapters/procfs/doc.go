// Package procfs implements the monitor port on top of the Linux /proc mount.
//
// The socket tables under <root>/net are the only source of endpoints and states, and the
// descriptor tables under <root>/<pid>/fd are the only source of their owners. Every scan is
// bounded by the domain limits, polls the context periodically and leaves the sockets it cannot
// attribute with a zero PID instead of failing the snapshot.
//
// Non-Linux builds get a service that reports domain.ErrUnsupported, so the wiring of the TUI
// does not need build tags of its own.
package procfs

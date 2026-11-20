// Package ports declares the interfaces the TUI consumes. Adapters implement
// them; the UI never imports a concrete adapter.
package ports

import (
	"context"

	"github.com/MiguelFPP/tui-port/internal/domain"
)

// MonitorService reads the network sockets of the host and terminates the
// processes that own them.
type MonitorService interface {
	Snapshot(ctx context.Context) (domain.Snapshot, error)
	Kill(ctx context.Context, pid int, signal domain.Signal) error
}

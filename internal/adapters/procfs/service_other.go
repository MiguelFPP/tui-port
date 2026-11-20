//go:build !linux

package procfs

import (
	"context"
	"fmt"
	"runtime"

	"github.com/MiguelFPP/tui-port/internal/domain"
	"github.com/MiguelFPP/tui-port/internal/ports"
)

// Service is the placeholder the non-Linux builds get: there is no /proc to read there.
type Service struct{}

var _ ports.MonitorService = (*Service)(nil)

// New always succeeds so the wiring of the TUI stays free of platform tags; every read reports
// domain.ErrUnsupported instead.
func New(root string) (*Service, error) {
	return &Service{}, nil
}

// Snapshot reports that the platform has no proc mount.
func (s *Service) Snapshot(ctx context.Context) (domain.Snapshot, error) {
	return domain.Snapshot{}, unsupported()
}

// Kill reports that the platform has no proc mount.
func (s *Service) Kill(ctx context.Context, pid int, signal domain.Signal) error {
	return unsupported()
}

// unsupported names the platform that has no adapter.
func unsupported() error {
	return fmt.Errorf("%w: %s", domain.ErrUnsupported, runtime.GOOS)
}

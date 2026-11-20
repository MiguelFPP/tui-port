//go:build linux

package procfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/MiguelFPP/tui-port/internal/domain"
	"github.com/MiguelFPP/tui-port/internal/ports"
)

// defaultRoot is the mount the adapter reads when New gets an empty root.
const defaultRoot = "/proc"

// maxNetTableBytes bounds one kernel socket table; a host with every socket fits far below it.
const maxNetTableBytes = 4 << 20

// netTables is the read order of the kernel tables, which the snapshot preserves.
var netTables = [...]struct {
	protocol domain.Protocol
	name     string
}{
	{protocol: domain.ProtocolTCP, name: "tcp"},
	{protocol: domain.ProtocolTCP6, name: "tcp6"},
	{protocol: domain.ProtocolUDP, name: "udp"},
	{protocol: domain.ProtocolUDP6, name: "udp6"},
}

// Service is the monitor port adapter over one proc mount.
type Service struct {
	root  string
	users *userCache
	kill  func(int, syscall.Signal) error
}

var _ ports.MonitorService = (*Service)(nil)

// New validates root and returns the adapter; an empty root means the /proc of the host.
func New(root string) (*Service, error) {
	dir := strings.TrimSpace(root)
	if dir == "" {
		dir = defaultRoot
	}

	info, err := os.Stat(dir)
	if err != nil {
		return nil, wrapRootError(dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %q no es un directorio", domain.ErrInvalidArgument, dir)
	}

	return &Service{root: dir, users: newUserCache(), kill: syscall.Kill}, nil
}

// Snapshot reads every socket of the mount plus the metadata of the processes that own one.
func (s *Service) Snapshot(ctx context.Context) (domain.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return domain.Snapshot{}, fmt.Errorf("procfs: Snapshot: %w", err)
	}

	sockets, truncated, err := s.readSockets(ctx)
	if err != nil {
		return domain.Snapshot{}, err
	}

	owners, ownersTruncated, err := readOwners(ctx, s.root)
	if err != nil {
		return domain.Snapshot{}, err
	}
	if ownersTruncated {
		truncated = true
	}

	for i := range sockets {
		if sockets[i].Inode == 0 {
			continue
		}
		if pid, ok := owners[sockets[i].Inode]; ok {
			sockets[i].PID = pid
		}
	}

	processes, err := s.readProcesses(ctx, ownerPIDs(sockets))
	if err != nil {
		return domain.Snapshot{}, err
	}

	return domain.Snapshot{
		TakenAt:   time.Now(),
		Sockets:   sockets,
		Processes: processes,
		Truncated: truncated,
	}, nil
}

// Kill sends the termination signal to the process instance in target, refusing
// the processes the monitor must never touch. The kernel start time is checked
// again immediately before the syscall, so a PID recycled after the snapshot is
// rejected instead of signaling an unrelated process.
func (s *Service) Kill(ctx context.Context, target domain.ProcessRef, signal domain.Signal) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("procfs: Kill: %w", err)
	}

	pid := target.PID
	if pid <= 0 {
		return fmt.Errorf("%w: pid %d", domain.ErrInvalidArgument, pid)
	}
	if pid == 1 || pid == os.Getpid() {
		return fmt.Errorf("%w: pid %d", domain.ErrProcessProtected, pid)
	}

	var signo syscall.Signal

	switch signal {
	case domain.SignalKill:
		signo = syscall.SIGKILL
	case domain.SignalTerm:
		signo = syscall.SIGTERM
	default:
		return fmt.Errorf("%w: señal %d", domain.ErrInvalidArgument, uint8(signal))
	}

	if target.StartTicks == 0 {
		return fmt.Errorf("%w: pid %d sin identidad verificada", domain.ErrProcessChanged, pid)
	}

	ticks, err := s.startTicks(pid)
	if err != nil {
		return err
	}
	if ticks != target.StartTicks {
		return fmt.Errorf("%w: pid %d", domain.ErrProcessChanged, pid)
	}

	kill := s.kill
	if kill == nil {
		return fmt.Errorf("%w: emisor de señales no configurado", domain.ErrUnavailable)
	}
	if err := kill(pid, signo); err != nil {
		return signalError(pid, err)
	}

	return nil
}

// startTicks reads the kernel start time of pid from the proc mount.
func (s *Service) startTicks(pid int) (uint64, error) {
	path := filepath.Join(s.root, strconv.Itoa(pid), "stat")

	data, _, err := readCapped(path, maxStatBytes)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return 0, fmt.Errorf("%w: pid %d", domain.ErrNotFound, pid)
		case errors.Is(err, fs.ErrPermission):
			return 0, fmt.Errorf("%w: pid %d", domain.ErrPermissionDenied, pid)
		default:
			return 0, fmt.Errorf("%w: pid %d: %w", domain.ErrUnavailable, pid, err)
		}
	}

	ticks, ok := parseStartTime(data)
	if !ok {
		return 0, fmt.Errorf("%w: pid %d", domain.ErrProcessChanged, pid)
	}

	return ticks, nil
}

// readSockets decodes the four kernel tables in order, tolerating the ones this kernel omits.
func (s *Service) readSockets(ctx context.Context) ([]domain.Socket, bool, error) {
	sockets := make([]domain.Socket, 0, 256)
	truncated, readable := false, false

	for _, table := range netTables {
		if err := ctx.Err(); err != nil {
			return nil, false, fmt.Errorf("procfs: Snapshot: %w", err)
		}

		data, cut, err := readCapped(filepath.Join(s.root, "net", table.name), maxNetTableBytes)
		if err != nil {
			continue // a kernel built without this family simply has no table
		}

		readable = true
		if cut {
			truncated = true
		}

		rows, dropped := parseNetFile(data, table.protocol, max(domain.MaxSockets-len(sockets), 0))
		sockets = append(sockets, rows...)

		if dropped {
			truncated = true
			break
		}
	}

	if !readable {
		return nil, false, fmt.Errorf("%w: %q no tiene tablas de sockets", domain.ErrUnavailable, s.root)
	}

	return sockets, truncated, nil
}

// readProcesses reads the metadata of the owning PIDs, which arrive ascending.
func (s *Service) readProcesses(ctx context.Context, pids []int) ([]domain.Process, error) {
	if len(pids) == 0 {
		return nil, nil
	}

	boot := readBootTime(s.root)
	processes := make([]domain.Process, 0, len(pids))

	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("procfs: Snapshot: %w", err)
		}

		processes = append(processes, readProcess(s.root, pid, boot, s.users))
	}

	return processes, nil
}

// signalError maps the refusals of the kernel onto the domain sentinels.
func signalError(pid int, err error) error {
	switch {
	case errors.Is(err, syscall.ESRCH):
		return fmt.Errorf("%w: pid %d", domain.ErrNotFound, pid)
	case errors.Is(err, syscall.EPERM), errors.Is(err, syscall.EACCES):
		return fmt.Errorf("%w: pid %d", domain.ErrPermissionDenied, pid)
	default:
		return fmt.Errorf("procfs: Kill: pid %d: %w", pid, err)
	}
}

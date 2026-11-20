package procfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/MiguelFPP/tui-port/internal/domain"
)

const (
	// ctxCheckEvery bounds how often the long scans poll the context.
	ctxCheckEvery = 64
	// maxStatusBytes and maxStatBytes bound the per-process files the adapter parses.
	maxStatusBytes = 64 << 10
	maxStatBytes   = 8 << 10
	// socketLinkPrefix and socketLinkSuffix frame the target of a socket descriptor.
	socketLinkPrefix = "socket:["
	socketLinkSuffix = "]"
	// statStartIndex is field 22 of /proc/<pid>/stat once the pid and the command name are gone.
	statStartIndex = 19
	// clockTicks is CLK_TCK, the rate the kernel counts starttime with.
	clockTicks = 100
)

// socketInode decodes the target of a /proc/<pid>/fd entry, "socket:[<inode>]".
func socketInode(target string) (uint64, bool) {
	if !strings.HasPrefix(target, socketLinkPrefix) || !strings.HasSuffix(target, socketLinkSuffix) {
		return 0, false
	}

	raw := target[len(socketLinkPrefix) : len(target)-len(socketLinkSuffix)]

	inode, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || inode == 0 {
		return 0, false
	}

	return inode, true
}

// pidOf reports whether the name of a <root> entry is a process id.
func pidOf(name string) (int, bool) {
	if name == "" {
		return 0, false
	}

	for _, digit := range name {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}

	pid, err := strconv.Atoi(name)
	if err != nil || pid <= 0 {
		return 0, false
	}

	return pid, true
}

// readOwners maps every socket inode to the first PID that holds a descriptor of it.
//
// A descriptor table that is unreadable, and a process that vanishes while it is listed, are
// ignored on purpose: the affected sockets stay unattributed instead of failing the snapshot.
func readOwners(ctx context.Context, root string) (map[uint64]int, bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, false, wrapRootError(root, err)
	}

	owners := make(map[uint64]int, 64)
	scanned, seen := 0, 0

	for _, entry := range entries {
		if seen%ctxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, false, fmt.Errorf("procfs: Snapshot: %w", err)
			}
		}
		seen++

		if !entry.IsDir() {
			continue
		}

		pid, ok := pidOf(entry.Name())
		if !ok {
			continue
		}
		if scanned >= domain.MaxProcesses {
			return owners, true, nil
		}
		scanned++

		collectInodes(filepath.Join(root, entry.Name(), "fd"), pid, owners)
	}

	return owners, false, nil
}

// collectInodes records the socket descriptors of one process, keeping the first owner.
func collectInodes(dir string, pid int, owners map[uint64]int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // the process is gone or its descriptors are not readable
	}
	if len(entries) > domain.MaxFdLinksPerProcess {
		entries = entries[:domain.MaxFdLinksPerProcess]
	}

	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		inode, ok := socketInode(target)
		if !ok {
			continue
		}
		if _, taken := owners[inode]; taken {
			continue
		}

		owners[inode] = pid
	}
}

// processStats is the content of /proc/<pid>/status the snapshot needs.
type processStats struct {
	name    string
	ppid    int
	uid     uint32
	hasUID  bool
	rss     uint64
	threads int
}

// parseStatus decodes the status fields the snapshot keeps.
func parseStatus(data []byte) processStats {
	var stats processStats

	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}

		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}

		switch key {
		case "Name":
			// Name is free text and may contain spaces and parentheses.
			stats.name = strings.TrimSpace(value)
		case "PPid":
			if ppid, err := strconv.Atoi(fields[0]); err == nil {
				stats.ppid = ppid
			}
		case "Uid":
			if uid, err := strconv.ParseUint(fields[0], 10, 32); err == nil {
				stats.uid = uint32(uid)
				stats.hasUID = true
			}
		case "VmRSS":
			if kib, err := strconv.ParseUint(fields[0], 10, 64); err == nil {
				stats.rss = kib * 1024 // the kernel reports kB, the domain wants bytes
			}
		case "Threads":
			if threads, err := strconv.Atoi(fields[0]); err == nil {
				stats.threads = threads
			}
		}
	}

	return stats
}

// parseCmdline turns the NUL separated argument vector into one line.
func parseCmdline(data []byte) string {
	args := make([]string, 0, 8)

	for _, arg := range bytes.Split(data, []byte{0}) {
		if len(arg) == 0 {
			continue
		}

		args = append(args, string(arg))
	}

	return strings.TrimSpace(strings.Join(args, " "))
}

// parseStartTime returns field 22 of /proc/<pid>/stat, counted in clock ticks after boot.
//
// The command name may contain spaces and parentheses, so the remaining fields only start after
// the last ')' of the line.
func parseStartTime(data []byte) (uint64, bool) {
	end := bytes.LastIndexByte(data, ')')
	if end < 0 {
		return 0, false
	}

	fields := strings.Fields(string(data[end+1:]))
	if len(fields) <= statStartIndex {
		return 0, false
	}

	ticks, err := strconv.ParseUint(fields[statStartIndex], 10, 64)
	if err != nil {
		return 0, false
	}

	return ticks, true
}

// parseBootTime returns the boot timestamp of the kernel in Unix seconds.
func parseBootTime(data []byte) (int64, bool) {
	for _, line := range strings.Split(string(data), "\n") {
		value, ok := strings.CutPrefix(line, "btime ")
		if !ok {
			continue
		}

		seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return 0, false
		}

		return seconds, true
	}

	return 0, false
}

// readBootTime returns the boot timestamp of the kernel, or the zero time when <root>/stat is
// unavailable.
func readBootTime(root string) time.Time {
	data, _, err := readCapped(filepath.Join(root, "stat"), maxStatBytes)
	if err != nil {
		return time.Time{}
	}

	seconds, ok := parseBootTime(data)
	if !ok {
		return time.Time{}
	}

	return time.Unix(seconds, 0).UTC()
}

// ownerPIDs lists the PIDs that own at least one socket, ascending and without duplicates.
func ownerPIDs(sockets []domain.Socket) []int {
	seen := make(map[int]struct{}, len(sockets))
	pids := make([]int, 0, len(sockets))

	for _, socket := range sockets {
		if socket.PID <= 0 {
			continue
		}
		if _, ok := seen[socket.PID]; ok {
			continue
		}

		seen[socket.PID] = struct{}{}
		pids = append(pids, socket.PID)
	}

	slices.Sort(pids)

	return pids
}

// readProcess builds the metadata of one PID. A file that cannot be read leaves its fields empty,
// so a process that exits between the socket scan and this read still gets a row.
func readProcess(root string, pid int, boot time.Time, users *userCache) domain.Process {
	dir := filepath.Join(root, strconv.Itoa(pid))
	process := domain.Process{PID: pid}

	var stats processStats
	if data, _, err := readCapped(filepath.Join(dir, "status"), maxStatusBytes); err == nil {
		stats = parseStatus(data)
	}

	process.Name = clip(stats.name, domain.MaxProcessNameBytes)
	process.PPID = stats.ppid
	process.RSS = stats.rss
	process.Threads = stats.threads
	if stats.hasUID {
		process.User = users.name(stats.uid)
	}

	if data, _, err := readCapped(filepath.Join(dir, "cmdline"), domain.MaxCmdlineBytes); err == nil {
		process.Cmdline = parseCmdline(data)
	}

	if boot.IsZero() {
		return process
	}

	if data, _, err := readCapped(filepath.Join(dir, "stat"), maxStatBytes); err == nil {
		if ticks, ok := parseStartTime(data); ok {
			process.StartedAt = boot.Add(time.Duration(ticks/clockTicks) * time.Second)
		}
	}

	return process
}

// userCache resolves UIDs to login names, caching hits and misses so a host with a slow NSS
// backend cannot stall every refresh. The lookup is a field so the tests never touch the local
// account database.
type userCache struct {
	lookup  func(id string) (string, error)
	mu      sync.Mutex
	entries map[uint32]string
}

// newUserCache returns a cache backed by the system account database.
func newUserCache() *userCache {
	return &userCache{lookup: lookupUsername, entries: make(map[uint32]string, 16)}
}

// lookupUsername wraps os/user so the cache never blocks NSS while holding its mutex.
func lookupUsername(id string) (string, error) {
	account, err := user.LookupId(id)
	if err != nil {
		return "", err
	}

	return account.Username, nil
}

// name returns the login name of uid, falling back to the numeric UID when the lookup fails or
// the cache is already full.
func (c *userCache) name(uid uint32) string {
	fallback := strconv.FormatUint(uint64(uid), 10)
	if c == nil || c.lookup == nil {
		return fallback
	}

	c.mu.Lock()
	name, cached := c.entries[uid]
	full := len(c.entries) >= domain.MaxUserLookups
	c.mu.Unlock()

	if cached {
		if name == "" {
			return fallback
		}

		return name
	}
	if full {
		return fallback
	}

	name = ""
	if resolved, err := c.lookup(fallback); err == nil {
		name = resolved
	}

	c.mu.Lock()
	if len(c.entries) < domain.MaxUserLookups {
		c.entries[uid] = name
	}
	c.mu.Unlock()

	if name == "" {
		return fallback
	}

	return name
}

// readCapped reads at most limit bytes of path, so a huge or hostile file cannot exhaust memory.
// The second result reports whether the file was longer than the limit.
func readCapped(path string, limit int) ([]byte, bool, error) {
	if limit <= 0 {
		return nil, false, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}

	data, readErr := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	closeErr := file.Close()

	if readErr != nil {
		return nil, false, readErr
	}
	if closeErr != nil {
		return nil, false, closeErr
	}

	if len(data) > limit {
		return data[:limit], true, nil
	}

	return data, false, nil
}

// clip bounds s to limit bytes without leaving a cut rune behind.
func clip(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}

	s = s[:limit]
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if r != utf8.RuneError || size > 1 {
			break
		}

		s = s[:len(s)-1]
	}

	return s
}

// wrapRootError maps a failed read of the mount root onto the domain sentinels.
func wrapRootError(root string, err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %q", domain.ErrNotFound, root)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%w: %q", domain.ErrPermissionDenied, root)
	default:
		return fmt.Errorf("%w: %q: %w", domain.ErrUnavailable, root, err)
	}
}

package procfs

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MiguelFPP/tui-port/internal/domain"
)

// statusFile renders the /proc/<pid>/status fields the adapter reads.
func statusFile(pid int, name string, ppid int, uid uint32, rssKiB uint64, threads int) string {
	uidText := strconv.FormatUint(uint64(uid), 10)

	return "Name:\t" + name + "\n" +
		"State:\tS (sleeping)\n" +
		"Tgid:\t" + strconv.Itoa(pid) + "\n" +
		"Pid:\t" + strconv.Itoa(pid) + "\n" +
		"PPid:\t" + strconv.Itoa(ppid) + "\n" +
		"Uid:\t" + uidText + "\t" + uidText + "\t" + uidText + "\t" + uidText + "\n" +
		"VmRSS:\t   " + strconv.FormatUint(rssKiB, 10) + " kB\n" +
		"Threads:\t" + strconv.Itoa(threads) + "\n"
}

// statFile renders /proc/<pid>/stat with starttime in field 22.
func statFile(pid int, comm string, startTicks uint64) string {
	// Fields 3 to 21: the state, the parent and the accounting counters the adapter ignores.
	fields := []string{
		"S", "1", "4242", "4242", "0", "-1", "4194304", "0", "0", "0",
		"0", "0", "0", "0", "20", "0", "1", "0", "0",
	}
	fields = append(fields, strconv.FormatUint(startTicks, 10))

	return strconv.Itoa(pid) + " (" + comm + ") " + strings.Join(fields, " ") + "\n"
}

// writeFixture creates a file of the fake proc root, parents included.
func writeFixture(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

// linkFixture creates a symlink of the fake proc root, parents included.
func linkFixture(t *testing.T, path, target string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("Symlink(%q, %q) error = %v", target, path, err)
	}
}

func TestParseStatus(t *testing.T) {
	stats := parseStatus([]byte(statusFile(4242, "odd ) name", 7, 1000, 12864, 9)))

	if stats.name != "odd ) name" {
		t.Errorf("name = %q, want %q", stats.name, "odd ) name")
	}
	if stats.ppid != 7 {
		t.Errorf("ppid = %d, want 7", stats.ppid)
	}
	if !stats.hasUID || stats.uid != 1000 {
		t.Errorf("uid = %d (%v), want 1000", stats.uid, stats.hasUID)
	}
	if stats.rss != 12864*1024 {
		t.Errorf("rss = %d, want %d", stats.rss, 12864*1024)
	}
	if stats.threads != 9 {
		t.Errorf("threads = %d, want 9", stats.threads)
	}
}

func TestParseStatusWithoutFields(t *testing.T) {
	for _, data := range []string{"", "Name:\tinit\n", "Uid:\n", "VmRSS:\tnope kB\n", "basura"} {
		stats := parseStatus([]byte(data))
		if stats.hasUID {
			t.Errorf("parseStatus(%q) hasUID = true, want false", data)
		}
		if stats.ppid != 0 || stats.rss != 0 || stats.threads != 0 {
			t.Errorf("parseStatus(%q) = %+v, want el valor cero", data, stats)
		}
	}
}

func TestParseCmdline(t *testing.T) {
	tests := []struct {
		data string
		want string
	}{
		{data: "", want: ""},
		{data: "\x00", want: ""},
		{data: "nginx\x00-g\x00daemon off;\x00", want: "nginx -g daemon off;"},
		{data: "a\x00\x00b\x00", want: "a b"},
		{data: "solo", want: "solo"},
	}

	for _, test := range tests {
		if got := parseCmdline([]byte(test.data)); got != test.want {
			t.Errorf("parseCmdline(%q) = %q, want %q", test.data, got, test.want)
		}
	}
}

func TestParseStartTime(t *testing.T) {
	got, ok := parseStartTime([]byte(statFile(4242, "odd ) name", 500)))
	if !ok {
		t.Fatal("parseStartTime() ok = false, want true")
	}
	if got != 500 {
		t.Errorf("parseStartTime() = %d, want 500", got)
	}

	rows := []string{
		"",
		"garbage",
		"4242 (no closing paren",
		"4242 (short) S 1 2 3",
		"4242 (bad) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 notanumber",
	}

	for _, row := range rows {
		if ticks, ok := parseStartTime([]byte(row)); ok {
			t.Errorf("parseStartTime(%q) = %d, want ok = false", row, ticks)
		}
	}
}

func TestParseBootTime(t *testing.T) {
	got, ok := parseBootTime([]byte("cpu  1 2 3\nbtime 1700000000\nprocesses 900\n"))
	if !ok {
		t.Fatal("parseBootTime() ok = false, want true")
	}
	if got != 1700000000 {
		t.Errorf("parseBootTime() = %d, want 1700000000", got)
	}

	for _, data := range []string{"", "intr 0\n", "btime nope\n", " btime 1\n"} {
		if seconds, ok := parseBootTime([]byte(data)); ok {
			t.Errorf("parseBootTime(%q) = %d, want ok = false", data, seconds)
		}
	}
}

func TestSocketInode(t *testing.T) {
	tests := []struct {
		target string
		want   uint64
		ok     bool
	}{
		{target: "socket:[1234567]", want: 1234567, ok: true},
		{target: "socket:[0]", ok: false},
		{target: "socket:[]", ok: false},
		{target: "socket:[12x]", ok: false},
		{target: "pipe:[123]", ok: false},
		{target: "/dev/null", ok: false},
		{target: "", ok: false},
	}

	for _, test := range tests {
		got, ok := socketInode(test.target)
		if ok != test.ok || got != test.want {
			t.Errorf("socketInode(%q) = %d, %v, want %d, %v", test.target, got, ok, test.want, test.ok)
		}
	}
}

func TestPIDOf(t *testing.T) {
	tests := []struct {
		name string
		pid  int
		ok   bool
	}{
		{name: "1", pid: 1, ok: true},
		{name: "4242", pid: 4242, ok: true},
		{name: "0", ok: false},
		{name: "", ok: false},
		{name: "net", ok: false},
		{name: "12a", ok: false},
		{name: "1_000", ok: false},
		{name: " 42", ok: false},
		{name: "99999999999999999999", ok: false},
	}

	for _, test := range tests {
		pid, ok := pidOf(test.name)
		if ok != test.ok || pid != test.pid {
			t.Errorf("pidOf(%q) = %d, %v, want %d, %v", test.name, pid, ok, test.pid, test.ok)
		}
	}
}

func TestOwnerPIDs(t *testing.T) {
	sockets := []domain.Socket{{PID: 4343}, {PID: 0}, {PID: 4242}, {PID: 4343}, {PID: -1}}

	got := ownerPIDs(sockets)
	if want := []int{4242, 4343}; !slices.Equal(got, want) {
		t.Errorf("ownerPIDs() = %v, want %v", got, want)
	}

	if got := ownerPIDs(nil); len(got) != 0 {
		t.Errorf("ownerPIDs(nil) = %v, want vacío", got)
	}
}

func TestUserCacheCachesHitsAndMisses(t *testing.T) {
	calls := 0

	cache := newUserCache()
	cache.lookup = func(id string) (string, error) {
		calls++
		if id == "0" {
			return "root", nil
		}

		return "", errors.New("uid desconocido")
	}

	if got := cache.name(0); got != "root" {
		t.Errorf("name(0) = %q, want %q", got, "root")
	}
	if got := cache.name(0); got != "root" {
		t.Errorf("name(0) = %q, want %q", got, "root")
	}
	if calls != 1 {
		t.Errorf("lookups = %d, want 1", calls)
	}

	if got := cache.name(4000); got != "4000" {
		t.Errorf("name(4000) = %q, want %q", got, "4000")
	}
	if got := cache.name(4000); got != "4000" {
		t.Errorf("name(4000) = %q, want %q", got, "4000")
	}
	if calls != 2 {
		t.Errorf("lookups = %d, want 2", calls)
	}
}

func TestUserCacheStopsAtTheLimit(t *testing.T) {
	calls := 0

	cache := newUserCache()
	cache.lookup = func(string) (string, error) {
		calls++

		return "", errors.New("uid desconocido")
	}

	for uid := uint32(1); uid <= domain.MaxUserLookups+10; uid++ {
		if got, want := cache.name(uid), strconv.FormatUint(uint64(uid), 10); got != want {
			t.Errorf("name(%d) = %q, want %q", uid, got, want)
		}
	}

	if calls != domain.MaxUserLookups {
		t.Errorf("lookups = %d, want %d", calls, domain.MaxUserLookups)
	}

	cache.mu.Lock()
	entries := len(cache.entries)
	cache.mu.Unlock()

	if entries != domain.MaxUserLookups {
		t.Errorf("entradas = %d, want %d", entries, domain.MaxUserLookups)
	}

	before := calls
	if got := cache.name(900000); got != "900000" {
		t.Errorf("name(900000) = %q, want %q", got, "900000")
	}
	if calls != before {
		t.Errorf("lookups = %d, want %d: una caché llena no consulta", calls, before)
	}
}

func TestUserCacheNilFallsBack(t *testing.T) {
	var cache *userCache

	if got := cache.name(1000); got != "1000" {
		t.Errorf("name(1000) = %q, want %q", got, "1000")
	}
}

func TestClip(t *testing.T) {
	tests := []struct {
		value string
		limit int
		want  string
	}{
		{value: "", limit: 8, want: ""},
		{value: "abc", limit: 8, want: "abc"},
		{value: "abcdef", limit: 3, want: "abc"},
		{value: "héllo", limit: 2, want: "h"},
		{value: "héllo", limit: 3, want: "hé"},
		{value: "abc", limit: 0, want: "abc"},
	}

	for _, test := range tests {
		if got := clip(test.value, test.limit); got != test.want {
			t.Errorf("clip(%q, %d) = %q, want %q", test.value, test.limit, got, test.want)
		}
	}
}

func TestReadProcessWithoutFiles(t *testing.T) {
	process := readProcess(t.TempDir(), 4242, time.Unix(1700000000, 0), newUserCache())

	if process.PID != 4242 {
		t.Errorf("PID = %d, want 4242", process.PID)
	}
	if process.Name != "" || process.Cmdline != "" || process.User != "" {
		t.Errorf("metadata = %+v, want vacía", process)
	}
	if process.RSS != 0 || process.Threads != 0 || process.PPID != 0 {
		t.Errorf("cuentas = %+v, want el valor cero", process)
	}
	if !process.StartedAt.IsZero() {
		t.Errorf("StartedAt = %v, want el valor cero", process.StartedAt)
	}
}

func TestReadProcessWithoutBootTime(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "4242", "stat"), statFile(4242, "job", 500))

	process := readProcess(root, 4242, time.Time{}, newUserCache())
	if !process.StartedAt.IsZero() {
		t.Errorf("StartedAt = %v, want el valor cero", process.StartedAt)
	}
}

func TestReadCapped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stat")
	writeFixture(t, path, "0123456789")

	data, cut, err := readCapped(path, 4)
	if err != nil {
		t.Fatalf("readCapped() error = %v, want nil", err)
	}
	if string(data) != "0123" || !cut {
		t.Errorf("readCapped() = %q, %v, want %q, true", data, cut, "0123")
	}

	data, cut, err = readCapped(path, 10)
	if err != nil {
		t.Fatalf("readCapped() error = %v, want nil", err)
	}
	if string(data) != "0123456789" || cut {
		t.Errorf("readCapped() = %q, %v, want el archivo completo y false", data, cut)
	}

	if _, _, err := readCapped(filepath.Join(t.TempDir(), "missing"), 4); err == nil {
		t.Error("readCapped() de un archivo ausente = nil, want un error")
	}
}

func FuzzParseStatus(f *testing.F) {
	f.Add([]byte("Name:\tnginx\nPPid:\t1\nUid:\t1000\t1000\t1000\t1000\nVmRSS:\t  1024 kB\nThreads:\t4\n"))
	f.Add([]byte(""))
	f.Add([]byte("Name:\x1b]52;c;SGVsbG8=\x07\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}

		_ = parseStatus(data)
	})
}

func FuzzParseCmdline(f *testing.F) {
	f.Add([]byte("sleep\x000--daemon\x00"))
	f.Add([]byte(""))
	f.Add([]byte("\x1b[2J\x00arg\x00"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}

		_ = parseCmdline(data)
	})
}

func FuzzParseStartTime(f *testing.F) {
	f.Add([]byte("1 (x) S " + strings.Repeat("0 ", 25)))
	f.Add([]byte(""))
	f.Add([]byte("1 (odd ) name) S " + strings.Repeat("7 ", 25)))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}

		_, _ = parseStartTime(data)
	})
}

func FuzzParseBootTime(f *testing.F) {
	f.Add([]byte("cpu 1 2 3 4\nbtime 1700000000\n"))
	f.Add([]byte(""))
	f.Add([]byte("btime no-es-numero\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}

		_, _ = parseBootTime(data)
	})
}

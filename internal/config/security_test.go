package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteDefaultDoesNotFollowSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs extra privileges on Windows")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	link := filepath.Join(dir, "config.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}

	if err := WriteDefault(link, true); err != nil {
		t.Fatalf("WriteDefault() error = %v", err)
	}

	targetData, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("os.ReadFile(target) error = %v", err)
	}
	if string(targetData) != "original" {
		t.Errorf("symlink target = %q, want it untouched", targetData)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("os.Lstat(link) error = %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("WriteDefault() followed the symlink instead of replacing it")
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config permissions = %v, want 0600", perm)
	}
}

func TestWriteDefaultCreatesAPrivateDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions do not apply on Windows")
	}

	dir := filepath.Join(t.TempDir(), "nested", "tui-port")
	if err := WriteDefault(filepath.Join(dir, "config.json"), false); err != nil {
		t.Fatalf("WriteDefault() error = %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o007 != 0 || perm&0o020 != 0 {
		t.Errorf("config directory permissions = %v, want no world access and no group write", perm)
	}
}

func FuzzDecodeFile(f *testing.F) {
	for _, seed := range []string{
		`{"theme":"dracula"}`,
		`{"keys":{"help":"h"}}`,
		`{"split_width":44,"split_collapsed":true}`,
		`{"theme":"x","unknown":true}`,
		``,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}

		_, _ = decodeFile(data, "fuzz")
	})
}

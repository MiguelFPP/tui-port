//go:build linux

package config

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestLoadFileRejectsFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("syscall.Mkfifo() error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, _, err := loadFile(path, true)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrConfigFile) {
			t.Fatalf("loadFile() error = %v, want %v", err, ErrConfigFile)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("loadFile() blocked on a FIFO")
	}
}

func TestLoadFileRejectsDevice(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("/dev/zero is not available")
	}

	_, _, err := loadFile("/dev/zero", true)
	if !errors.Is(err, ErrConfigFile) {
		t.Fatalf("loadFile(/dev/zero) error = %v, want %v", err, ErrConfigFile)
	}
}

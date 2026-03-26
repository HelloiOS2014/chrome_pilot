package tmpfile

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Manager manages temporary files in a directory, cleaning them based on age.
type Manager struct {
	dir    string
	maxAge time.Duration
}

// NewManager creates a Manager for the given directory. The directory is
// created if it does not exist. maxAge is the default age threshold used
// by AutoClean; individual Clean/DryRun calls accept their own duration.
func NewManager(dir string, maxAge time.Duration) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("tmpfile: mkdir %s: %w", dir, err)
	}
	return &Manager{dir: dir, maxAge: maxAge}, nil
}

// DryRun returns the number of files and total bytes that would be deleted
// if Clean were called with the same duration.
func (m *Manager) DryRun(olderThan time.Duration) (count int, size int64) {
	cutoff := time.Now().Add(-olderThan)
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			count++
			size += info.Size()
		}
	}
	return count, size
}

// Clean deletes files older than olderThan and returns the number deleted,
// bytes freed, and the first error encountered (if any).
func (m *Manager) Clean(olderThan time.Duration) (count int, freed int64, firstErr error) {
	cutoff := time.Now().Add(-olderThan)
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return 0, 0, fmt.Errorf("tmpfile: readdir %s: %w", m.dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			size := info.Size()
			path := filepath.Join(m.dir, e.Name())
			if err := os.Remove(path); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			count++
			freed += size
		}
	}
	return count, freed, firstErr
}

// CleanAll deletes every file in the directory (not subdirectories).
func (m *Manager) CleanAll() (count int, freed int64, firstErr error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return 0, 0, fmt.Errorf("tmpfile: readdir %s: %w", m.dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		size := info.Size()
		path := filepath.Join(m.dir, e.Name())
		if err := os.Remove(path); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		count++
		freed += size
	}
	return count, freed, firstErr
}

// AutoClean deletes files older than the maxAge configured at construction time.
func (m *Manager) AutoClean() (count int, freed int64, err error) {
	return m.Clean(m.maxAge)
}

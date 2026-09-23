package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RotatingFile is an io.Writer that appends to a date-named file
// (vndl-YYYY-MM-DD.log, UTC) under dir, rotating at day boundaries and
// pruning files older than retentionDays on a periodic sweep.
type RotatingFile struct {
	dir           string
	retentionDays int

	mu       sync.Mutex
	file     *os.File
	fileDate string
}

// NewRotatingFile creates the log directory/file lazily on first Write, so
// a bad LOG_DIR never fails startup on its own.
func NewRotatingFile(dir string, retentionDays int) *RotatingFile {
	rf := &RotatingFile{dir: dir, retentionDays: retentionDays}
	if retentionDays > 0 {
		go rf.sweepLoop()
	}
	return rf
}

// EnsureWritable opens today's log file immediately, surfacing a
// permission/path problem at startup instead of silently on first Write.
func (rf *RotatingFile) EnsureWritable() error {
	_, err := rf.Write(nil)
	return err
}

func (rf *RotatingFile) Write(p []byte) (int, error) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	today := time.Now().UTC().Format("2006-01-02")
	if rf.file == nil || rf.fileDate != today {
		if err := rf.rotateLocked(today); err != nil {
			return 0, err
		}
	}
	return rf.file.Write(p)
}

func (rf *RotatingFile) rotateLocked(date string) error {
	if err := os.MkdirAll(rf.dir, 0o700); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	path := filepath.Join(rf.dir, "vndl-"+date+".log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // owner-only: carries client IPs
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	if rf.file != nil {
		_ = rf.file.Close()
	}
	rf.file = f
	rf.fileDate = date
	return nil
}

func (rf *RotatingFile) sweepLoop() {
	ticker := time.NewTicker(time.Hour)
	for range ticker.C {
		rf.pruneOld()
	}
}

// pruneOld deletes log files whose date, parsed from the filename, is
// older than the retention window.
func (rf *RotatingFile) pruneOld() {
	entries, err := os.ReadDir(rf.dir)
	if err != nil {
		return
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -rf.retentionDays)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "vndl-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		dateStr := strings.TrimSuffix(strings.TrimPrefix(name, "vndl-"), ".log")
		fileDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if fileDate.Before(cutoff) {
			_ = os.Remove(filepath.Join(rf.dir, name))
		}
	}
}

func (rf *RotatingFile) Close() error {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.file != nil {
		return rf.file.Close()
	}
	return nil
}

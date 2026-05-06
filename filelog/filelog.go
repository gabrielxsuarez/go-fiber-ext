// Package filelog provides rolling file loggers with sensible defaults.
//
// Four built-in loggers are available as typed methods: Access, Warning, Error,
// and Event. Each one creates its log file lazily — only when the method is
// called for the first time. The Error logger also writes to os.Stderr.
//
// For anything beyond the four built-in loggers, use the generic Log method,
// which creates a "<name>.log" file on first use.
package filelog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Config controls the rotation policy for all loggers managed by a FileLog
// instance. Zero values are replaced with sensible defaults.
type Config struct {
	// MaxSize is the maximum size in megabytes before a log file is rotated.
	// Default: 100.
	MaxSize int

	// MaxBackups is the maximum number of old log files to keep.
	// Default: 5.
	MaxBackups int

	// MaxAge is the maximum number of days to retain old log files.
	// Default: 0 (no age-based removal; only MaxBackups applies).
	MaxAge int

	// Compress determines whether rotated log files should be gzipped.
	// Default: true.
	Compress *bool
}

func (c Config) withDefaults() Config {
	if c.MaxSize == 0 {
		c.MaxSize = 100
	}
	if c.MaxBackups == 0 {
		c.MaxBackups = 5
	}
	if c.Compress == nil {
		v := true
		c.Compress = &v
	}
	return c
}

// FileLog manages a set of rolling file loggers under a single directory.
type FileLog struct {
	dir     string
	cfg     Config
	loggers sync.Map // map[string]*log.Logger
	closers sync.Map // map[string]io.Closer
}

// New creates a FileLog that writes into dir. The directory is created
// (with parents) if it doesn't exist. An optional Config controls the
// rotation policy for every logger; zero values use sensible defaults.
func New(dir string, cfgs ...Config) *FileLog {
	var cfg Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	cfg = cfg.withDefaults()

	os.MkdirAll(dir, 0o755)

	return &FileLog{dir: dir, cfg: cfg}
}

// Access writes to access.log (created on first call).
func (fl *FileLog) Access(format string, args ...any) {
	fl.getOrCreate("access", false).Printf(format, args...)
}

// Warning writes to warning.log (created on first call).
func (fl *FileLog) Warning(format string, args ...any) {
	fl.getOrCreate("warning", false).Printf(format, args...)
}

// Error writes to error.log AND os.Stderr (created on first call).
func (fl *FileLog) Error(format string, args ...any) {
	fl.getOrCreate("error", true).Printf(format, args...)
}

// Event writes to events.log (created on first call).
func (fl *FileLog) Event(format string, args ...any) {
	fl.getOrCreate("events", false).Printf(format, args...)
}

// Log writes to <name>.log (created on first call). This is the escape hatch
// for loggers beyond the four built-in ones.
func (fl *FileLog) Log(name string, format string, args ...any) {
	fl.getOrCreate(name, false).Printf(format, args...)
}

// Close closes all log files opened by this FileLog instance.
func (fl *FileLog) Close() error {
	var firstErr error
	fl.closers.Range(func(key, value any) bool {
		if err := value.(io.Closer).Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		fl.closers.Delete(key)
		fl.loggers.Delete(key)
		return true
	})
	return firstErr
}

func (fl *FileLog) getOrCreate(name string, withStderr bool) *log.Logger {
	if v, ok := fl.loggers.Load(name); ok {
		return v.(*log.Logger)
	}

	filename := filepath.Join(fl.dir, fmt.Sprintf("%s.log", name))
	lj := &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    fl.cfg.MaxSize,
		MaxBackups: fl.cfg.MaxBackups,
		MaxAge:     fl.cfg.MaxAge,
		Compress:   *fl.cfg.Compress,
	}
	var w io.Writer = lj
	if withStderr {
		w = io.MultiWriter(os.Stderr, w)
	}

	l := log.New(w, "", log.LstdFlags)
	actual, loaded := fl.loggers.LoadOrStore(name, l)
	if loaded {
		_ = lj.Close()
		return actual.(*log.Logger)
	}
	fl.closers.Store(name, lj)
	return l
}

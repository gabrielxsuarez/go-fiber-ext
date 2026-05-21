// Package filelog provides rolling structured file loggers with sensible
// defaults.
//
// Four built-in loggers are available as typed methods: Access, Warning, Error,
// and Event. Each one creates its log file lazily — only when the method is
// called for the first time. The Error logger also writes to os.Stderr.
//
// Log files keep the historical ".log" names, but each line is emitted as JSON
// through slog.JSONHandler. For anything beyond the four built-in loggers, use
// the generic Log method, which creates a "<name>.log" file on first use.
package filelog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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

	// App is written as the "app" attribute in every structured log entry.
	// Default: base name of the log directory.
	App string
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
	app     string
	loggers sync.Map // map[string]*slog.Logger
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
	app := cfg.App
	if app == "" {
		app = filepath.Base(filepath.Clean(dir))
	}

	return &FileLog{dir: dir, cfg: cfg, app: app}
}

// Access writes to access.log (created on first call).
func (fl *FileLog) Access(format string, args ...any) {
	fl.logf("access", slog.LevelInfo, "access", format, args...)
}

// Warning writes to warning.log (created on first call).
func (fl *FileLog) Warning(format string, args ...any) {
	fl.logf("warning", slog.LevelWarn, "warning", format, args...)
}

// Error writes to error.log AND os.Stderr (created on first call).
func (fl *FileLog) Error(format string, args ...any) {
	fl.logf("error", slog.LevelError, "error", format, args...)
}

// Event writes to events.log (created on first call).
func (fl *FileLog) Event(format string, args ...any) {
	fl.logf("events", slog.LevelInfo, "event", format, args...)
}

// Log writes to <name>.log (created on first call). This is the escape hatch
// for loggers beyond the four built-in ones.
func (fl *FileLog) Log(name string, format string, args ...any) {
	fl.logf(name, slog.LevelInfo, name, format, args...)
}

// AccessAttrs writes a structured access event to access.log.
func (fl *FileLog) AccessAttrs(msg string, attrs ...slog.Attr) {
	fl.logAttrs("access", slog.LevelInfo, msg, attrs...)
}

// WarningAttrs writes a structured warning event to warning.log.
func (fl *FileLog) WarningAttrs(msg string, attrs ...slog.Attr) {
	fl.logAttrs("warning", slog.LevelWarn, msg, attrs...)
}

// ErrorAttrs writes a structured error event to error.log and os.Stderr.
func (fl *FileLog) ErrorAttrs(msg string, attrs ...slog.Attr) {
	fl.logAttrs("error", slog.LevelError, msg, attrs...)
}

// EventAttrs writes a structured business/operational event to events.log.
func (fl *FileLog) EventAttrs(msg string, attrs ...slog.Attr) {
	fl.logAttrs("events", slog.LevelInfo, msg, attrs...)
}

// LogAttrs writes a structured event to <name>.log.
func (fl *FileLog) LogAttrs(name string, msg string, attrs ...slog.Attr) {
	fl.logAttrs(name, slog.LevelInfo, msg, attrs...)
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

func (fl *FileLog) logf(name string, level slog.Level, msg string, format string, args ...any) {
	fl.logAttrs(name, level, fmt.Sprintf(format, args...))
}

func (fl *FileLog) logAttrs(name string, level slog.Level, msg string, attrs ...slog.Attr) {
	logger := fl.getOrCreate(name, name == "error")
	logger.LogAttrs(context.Background(), level, msg, attrs...)
}

func (fl *FileLog) getOrCreate(name string, withStderr bool) *slog.Logger {
	if v, ok := fl.loggers.Load(name); ok {
		return v.(*slog.Logger)
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

	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	l := slog.New(handler).With(
		slog.String("app", fl.app),
		slog.String("log", name),
	)
	actual, loaded := fl.loggers.LoadOrStore(name, l)
	if loaded {
		_ = lj.Close()
		return actual.(*slog.Logger)
	}
	fl.closers.Store(name, lj)
	return l
}

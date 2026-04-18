// Package logger provides a level-based logger with optional per-module overrides.
//
// Configuration via LOG_LEVEL environment variable:
//
//	LOG_LEVEL=debug               — all modules at debug
//	LOG_LEVEL=info,srn=debug      — global info, srn module at debug
//	LOG_LEVEL=debug,llm=warn,rss=warn — global debug, silence llm and rss
//
// Module names are short lowercase identifiers: srn, rss, llm, cache,
// aggregate, qbit, disk, prowlarr, sonarr, proxy, tmdb.
// Call logger.For("module") to get a module-scoped logger.
package logger

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Level represents the log verbosity level.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

var (
	current         = LevelInfo
	moduleOverrides = map[string]Level{}
	knownModules    = map[string]bool{}
	mu              sync.RWMutex
)

func parseLevel(s string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug, true
	case "info":
		return LevelInfo, true
	case "warn", "warning":
		return LevelWarn, true
	case "error":
		return LevelError, true
	}
	return LevelInfo, false
}

// Init reads LOG_LEVEL from the environment and configures log levels.
// Format: "global[,module=level,...]"
// Examples:
//
//	LOG_LEVEL=debug
//	LOG_LEVEL=info,srn=debug,llm=warn
func Init() {
	raw := strings.TrimSpace(os.Getenv("LOG_LEVEL"))
	if raw == "" {
		fmt.Printf("🔧 [Logger] No LOG_LEVEL set, using default (global=%s)\n", current)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	overrides := map[string]Level{}
	for _, token := range strings.Split(raw, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if idx := strings.IndexByte(token, '='); idx != -1 {
			mod := strings.ToLower(strings.TrimSpace(token[:idx]))
			if l, ok := parseLevel(token[idx+1:]); ok {
				overrides[mod] = l
			}
		} else {
			if l, ok := parseLevel(token); ok {
				current = l
			}
		}
	}
	moduleOverrides = overrides

	// Validate module names: warn on any name not registered via For().
	// knownModules is populated by For() during package init, which always
	// runs before Init() is called from main(). Unknown names are almost
	// certainly typos — surface them immediately so the user can correct them.
	var badModules []string
	for mod := range overrides {
		if !knownModules[mod] {
			badModules = append(badModules, mod)
		}
	}
	if len(badModules) > 0 {
		sort.Strings(badModules)
		var known []string
		for k := range knownModules {
			known = append(known, k)
		}
		sort.Strings(known)
		fmt.Fprintf(os.Stderr, "❌ [Logger] 未知模块名: %s\n   可用模块: %s\n",
			strings.Join(badModules, ", "), strings.Join(known, ", "))
	}

	// Print confirmation to stdout so user knows the level was received
	fmt.Printf("🔧 [Logger] Initialized with LOG_LEVEL=%q (global=%s, overrides=%v)\n", raw, current, moduleOverrides)
}

// SetLevel overrides the global log level (useful in tests).
func SetLevel(l Level) {
	mu.Lock()
	current = l
	mu.Unlock()
}

// GetLevel returns the current global log level.
func GetLevel() Level {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// effectiveLevel returns the level for a module, falling back to global.
func effectiveLevel(module string) Level {
	mu.RLock()
	defer mu.RUnlock()
	if l, ok := moduleOverrides[module]; ok {
		return l
	}
	return current
}

// logf formats a message with timestamp and level, ensures it ends with exactly one newline,
// and writes it to w. Consecutive newlines in the formatted output are collapsed to one.
func logf(w io.Writer, level Level, module string, format string, args ...any) {
	now := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf(format, args...)

	// Prefix with timestamp and level
	prefix := fmt.Sprintf("%s [%-5s] ", now, level.String())
	if module != "" {
		prefix += fmt.Sprintf("[%s] ", module)
	}

	fullMsg := prefix + msg

	// Collapse consecutive newlines to a single newline
	for strings.Contains(fullMsg, "\n\n") {
		fullMsg = strings.ReplaceAll(fullMsg, "\n\n", "\n")
	}
	// Ensure the message ends with exactly one newline
	fullMsg = strings.TrimRight(fullMsg, "\n") + "\n"
	fmt.Fprint(w, fullMsg)
}

// ── Global functions (module = "") ───────────────────────────────────────────

// Debug logs at DEBUG level using the global level.
func Debug(format string, args ...any) {
	if current <= LevelDebug {
		logf(os.Stdout, LevelDebug, "", format, args...)
	}
}

// Info logs at INFO level.
func Info(format string, args ...any) {
	if current <= LevelInfo {
		logf(os.Stdout, LevelInfo, "", format, args...)
	}
}

// Warn logs a recoverable error.
func Warn(format string, args ...any) {
	if current <= LevelWarn {
		logf(os.Stdout, LevelWarn, "", format, args...)
	}
}

// Error always logs to stderr.
func Error(format string, args ...any) {
	logf(os.Stderr, LevelError, "", format, args...)
}

// ── ModuleLogger ─────────────────────────────────────────────────────────────

// ModuleLogger is a logger scoped to a named module.
// Obtain one via For("module").
type ModuleLogger struct {
	module string
}

// For returns a ModuleLogger for the given module name and registers the name
// so that Init() can validate LOG_LEVEL overrides against known modules.
// Module names should be short lowercase identifiers (e.g. "rss", "engine").
func For(module string) *ModuleLogger {
	m := strings.ToLower(module)
	mu.Lock()
	knownModules[m] = true
	mu.Unlock()
	return &ModuleLogger{module: m}
}

func (m *ModuleLogger) Debug(format string, args ...any) {
	level := effectiveLevel(m.module)
	if level <= LevelDebug {
		logf(os.Stdout, LevelDebug, m.module, format, args...)
	}
}

func (m *ModuleLogger) Info(format string, args ...any) {
	level := effectiveLevel(m.module)
	if level <= LevelInfo {
		logf(os.Stdout, LevelInfo, m.module, format, args...)
	}
}

func (m *ModuleLogger) Warn(format string, args ...any) {
	level := effectiveLevel(m.module)
	if level <= LevelWarn {
		logf(os.Stdout, LevelWarn, m.module, format, args...)
	}
}

func (m *ModuleLogger) Error(format string, args ...any) {
	logf(os.Stderr, LevelError, m.module, format, args...)
}

func (m *ModuleLogger) Printf(format string, args ...any) {
	m.Info(format, args...)
}

// Package logging provides structured, leveled logging with function tracing.
// Every function in the system should log entry/exit at DEBUG level for traceability.
package logging

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level defines the severity of a log message.
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
	FATAL
)

func (l Level) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	case FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// Logger is a structured logger with leveled output and field support.
type Logger struct {
	mu       sync.Mutex
	level    Level
	output   io.Writer
	fields   map[string]interface{}
	appName  string
}

// NewLogger creates a new logger with the given minimum level and output writer.
func NewLogger(level Level, output io.Writer) *Logger {
	return &Logger{
		level:   level,
		output:  output,
		fields:  make(map[string]interface{}),
		appName: "cortex",
	}
}

// DefaultLogger is the package-level logger used throughout the system.
var DefaultLogger = NewLogger(DEBUG, os.Stderr)

// SetLevel changes the logging threshold.
func SetLevel(l Level) {
	DefaultLogger.mu.Lock()
	defer DefaultLogger.mu.Unlock()
	DefaultLogger.level = l
}

// WithFields returns a new logger with the given fields pre-attached.
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	newFields := make(map[string]interface{})
	for k, v := range l.fields {
		newFields[k] = v
	}
	for k, v := range fields {
		newFields[k] = v
	}
	return &Logger{
		level:   l.level,
		output:  l.output,
		fields:  newFields,
		appName: l.appName,
	}
}

// log writes a structured log line.
func (l *Logger) log(level Level, msg string, fields map[string]interface{}) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Build field string
	var fieldParts []string
	for k, v := range l.fields {
		fieldParts = append(fieldParts, fmt.Sprintf("%s=%v", k, v))
	}
	for k, v := range fields {
		fieldParts = append(fieldParts, fmt.Sprintf("%s=%v", k, v))
	}

	// Get caller info
	_, file, line, ok := runtime.Caller(2)
	caller := "???"
	if ok {
		// Trim to just the relevant path
		short := file
		if idx := strings.LastIndex(file, "/cortex/"); idx >= 0 {
			short = file[idx+8:]
		}
		caller = fmt.Sprintf("%s:%d", short, line)
	}

	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	fieldStr := ""
	if len(fieldParts) > 0 {
		fieldStr = " " + strings.Join(fieldParts, " ")
	}

	fmt.Fprintf(l.output, "%s [%s] %-30s %s%s\n", timestamp, level.String(), caller, msg, fieldStr)
}

// Debug logs a debug-level message. Use for function entry/exit and detailed tracing.
func (l *Logger) Debug(msg string, fields ...map[string]interface{}) {
	f := mergeFields(fields...)
	l.log(DEBUG, msg, f)
}

// Info logs an info-level message. Use for normal operational events.
func (l *Logger) Info(msg string, fields ...map[string]interface{}) {
	f := mergeFields(fields...)
	l.log(INFO, msg, f)
}

// Warn logs a warning-level message. Use for unexpected but non-fatal events.
func (l *Logger) Warn(msg string, fields ...map[string]interface{}) {
	f := mergeFields(fields...)
	l.log(WARN, msg, f)
}

// Error logs an error-level message. Use for errors that need attention.
func (l *Logger) Error(msg string, fields ...map[string]interface{}) {
	f := mergeFields(fields...)
	l.log(ERROR, msg, f)
}

// Fatal logs a fatal-level message and exits the process.
func (l *Logger) Fatal(msg string, fields ...map[string]interface{}) {
	f := mergeFields(fields...)
	l.log(FATAL, msg, f)
	os.Exit(1)
}

// Trace logs function entry with a "→" prefix and returns a function
// that logs exit with a "←" prefix. Usage: defer logger.Trace(ctx)()
func (l *Logger) Trace(msg string, fields ...map[string]interface{}) func() {
	f := mergeFields(fields...)
	start := time.Now()
	l.Debug("→ "+msg, f)
	return func() {
		duration := time.Since(start)
		l.Debug("← "+msg, mergeFields(f, map[string]interface{}{"duration": duration.String()}))
	}
}

// Package-level convenience functions.

func Debug(msg string, fields ...map[string]interface{}) {
	DefaultLogger.Debug(msg, fields...)
}

func Info(msg string, fields ...map[string]interface{}) {
	DefaultLogger.Info(msg, fields...)
}

func Warn(msg string, fields ...map[string]interface{}) {
	DefaultLogger.Warn(msg, fields...)
}

func Error(msg string, fields ...map[string]interface{}) {
	DefaultLogger.Error(msg, fields...)
}

func Fatal(msg string, fields ...map[string]interface{}) {
	DefaultLogger.Fatal(msg, fields...)
}

func Trace(msg string, fields ...map[string]interface{}) func() {
	return DefaultLogger.Trace(msg, fields...)
}

// mergeFields combines multiple field maps (last one wins for duplicate keys).
func mergeFields(fields ...map[string]interface{}) map[string]interface{} {
	if len(fields) == 0 {
		return nil
	}
	if len(fields) == 1 {
		return fields[0]
	}
	result := make(map[string]interface{})
	for _, f := range fields {
		for k, v := range f {
			result[k] = v
		}
	}
	return result
}

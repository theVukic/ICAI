package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type applicationLogger struct {
	mu          sync.Mutex
	logDir      string
	sessionFile *os.File
	sessionPath string
	appLog      *log.Logger
	eventLog    *log.Logger
}

type clientLogEvent struct {
	Level     string         `json:"level"`
	Category  string         `json:"category"`
	Message   string         `json:"message"`
	Path      string         `json:"path"`
	Details   map[string]any `json:"details"`
	Timestamp string         `json:"timestamp"`
}

var runtimeLogger *applicationLogger

func initApplicationLogger(searchRoots []string) error {
	logger, err := newApplicationLogger(searchRoots)
	if err != nil {
		return err
	}

	runtimeLogger = logger
	log.SetFlags(0)
	log.SetOutput(logger.appWriter())
	log.Printf("[%s] [BOOT] logging initialized log_dir=%s session_log=%s", timestampNow(), logger.logDir, logger.sessionPath)
	return nil
}

func newApplicationLogger(searchRoots []string) (*applicationLogger, error) {
	logDir, err := resolveLogDirectory(searchRoots)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("ne mogu kreirati log folder %s: %w", logDir, err)
	}

	sessionPath := filepath.Join(logDir, sessionLogFileName(time.Now()))

	sessionFile, err := os.OpenFile(sessionPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("ne mogu otvoriti session log: %w", err)
	}

	return &applicationLogger{
		logDir:      logDir,
		sessionFile: sessionFile,
		sessionPath: sessionPath,
		appLog:      log.New(io.MultiWriter(os.Stdout, sessionFile), "", 0),
		eventLog:    log.New(io.MultiWriter(os.Stdout, sessionFile), "", 0),
	}, nil
}

func (l *applicationLogger) appWriter() io.Writer {
	if l == nil {
		return os.Stdout
	}
	return logWriterFunc(func(p []byte) (int, error) {
		message := strings.TrimRight(string(p), "\r\n")
		l.appLog.Printf("[%s] %s", timestampNow(), message)
		return len(p), nil
	})
}

func (l *applicationLogger) close() {
	if l == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.sessionFile != nil {
		_ = l.sessionFile.Close()
		l.sessionFile = nil
	}
}

func logEvent(category string, message string, details map[string]any) {
	if runtimeLogger == nil {
		return
	}

	if details == nil {
		details = map[string]any{}
	}

	runtimeLogger.eventLog.Printf(
		"[%s] [EVENT] [%s] %s%s",
		timestampNow(),
		strings.ToUpper(strings.TrimSpace(category)),
		strings.TrimSpace(message),
		formatLogFields(details),
	)
}

func timestampNow() string {
	return time.Now().Format(time.RFC3339Nano)
}

func resolveLogDirectory(searchRoots []string) (string, error) {
	candidates := make([]string, 0, len(searchRoots)+1)
	for _, root := range searchRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}

		if filepath.Ext(root) != "" {
			root = filepath.Dir(root)
		}

		if strings.EqualFold(filepath.Base(root), "backend") {
			candidates = append(candidates, filepath.Join(filepath.Dir(root), "log"))
		}
		candidates = append(candidates, filepath.Join(root, "log"))
	}
	candidates = append(candidates, filepath.Join(".", "log"))

	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if err := os.MkdirAll(candidate, 0o755); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("nije moguće odrediti log folder")
}

func sessionLogFileName(now time.Time) string {
	return "icai-session-" + now.Format("2006-01-02_15-04-05") + ".log"
}

func formatLogFields(fields map[string]any) string {
	if len(fields) == 0 {
		return ""
	}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, formatLogValue(fields[key])))
	}

	return " " + strings.Join(parts, " ")
}

func formatLogValue(value any) string {
	switch typed := value.(type) {
	case string:
		if strings.ContainsAny(typed, " \t") {
			return fmt.Sprintf("%q", typed)
		}
		return typed
	case []string:
		encoded, _ := json.Marshal(typed)
		return string(encoded)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprintf("%v", typed)
		}
		return string(encoded)
	}
}

func maskSecret(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 8 {
		return "****"
	}
	return trimmed[:4] + "..." + trimmed[len(trimmed)-4:]
}

func requestClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		return forwarded
	}
	return r.RemoteAddr
}

type logWriterFunc func([]byte) (int, error)

func (f logWriterFunc) Write(p []byte) (int, error) {
	return f(p)
}

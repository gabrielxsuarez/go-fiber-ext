package filelog

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyMethodsWriteJSONLines(t *testing.T) {
	dir := t.TempDir()
	fl := New(dir, Config{App: "test-app"})
	fl.Event("deploy %s", "ok")
	fl.Warning("slow %d", 12)
	fl.Error("boom %s", "db")

	if err := fl.Close(); err != nil {
		t.Fatalf("close logs: %v", err)
	}

	event := readJSONLine(t, filepath.Join(dir, "events.log"))
	if event["app"] != "test-app" || event["log"] != "events" || event["level"] != "INFO" || event["msg"] != "deploy ok" {
		t.Fatalf("unexpected event log: %#v", event)
	}

	warning := readJSONLine(t, filepath.Join(dir, "warning.log"))
	if warning["level"] != "WARN" || warning["msg"] != "slow 12" {
		t.Fatalf("unexpected warning log: %#v", warning)
	}

	errorLog := readJSONLine(t, filepath.Join(dir, "error.log"))
	if errorLog["level"] != "ERROR" || errorLog["msg"] != "boom db" {
		t.Fatalf("unexpected error log: %#v", errorLog)
	}
}

func TestStructuredAttrs(t *testing.T) {
	dir := t.TempDir()
	fl := New(dir, Config{App: "test-app"})
	fl.AccessAttrs("http_request", slog.Int("status", 200), slog.String("path", "/health"))

	if err := fl.Close(); err != nil {
		t.Fatalf("close logs: %v", err)
	}

	access := readJSONLine(t, filepath.Join(dir, "access.log"))
	if access["msg"] != "http_request" || access["status"].(float64) != 200 || access["path"] != "/health" {
		t.Fatalf("unexpected access log: %#v", access)
	}
}

func readJSONLine(t *testing.T, path string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var entry map[string]any
	if err := json.Unmarshal(content, &entry); err != nil {
		t.Fatalf("decode JSON %s: %v\n%s", path, err, content)
	}
	return entry
}

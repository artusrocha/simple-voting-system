package logutil

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestConfigureRespectsLogLevel(t *testing.T) {
	var buf bytes.Buffer
	logger, err := Configure("api", "info", &buf)
	if err != nil {
		t.Fatalf("configure logger: %v", err)
	}

	logger.DebugContext(context.Background(), "debug hidden", "voteId", "v-1")
	logger.InfoContext(context.Background(), "info visible", "voteId", "v-1")

	output := buf.String()
	if strings.Contains(output, "debug hidden") {
		t.Fatalf("expected debug log to be filtered, got %q", output)
	}
	if !strings.Contains(output, "info visible") {
		t.Fatalf("expected info log to be present, got %q", output)
	}
	if !strings.Contains(output, "\"service\":\"api\"") {
		t.Fatalf("expected service field in log output, got %q", output)
	}
}

func TestConfigureProducesStructuredErrorLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := MustConfigure("api", "debug", &buf)

	logger.ErrorContext(context.Background(), "vote delivery failed", "voteId", "vote-123", "topic", "votes.raw", "error", "broker unavailable")

	output := buf.String()
	for _, want := range []string{
		"\"level\":\"ERROR\"",
		"vote delivery failed",
		"\"voteId\":\"vote-123\"",
		"\"topic\":\"votes.raw\"",
		"\"error\":\"broker unavailable\"",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in log output, got %q", want, output)
		}
	}
}

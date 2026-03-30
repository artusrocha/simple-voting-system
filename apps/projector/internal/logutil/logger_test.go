package logutil

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestConfigureRespectsLogLevel(t *testing.T) {
	var buf bytes.Buffer
	logger, err := Configure("projector", "warn", &buf)
	if err != nil {
		t.Fatalf("configure logger: %v", err)
	}

	logger.InfoContext(context.Background(), "info hidden", "topic", "votes.raw")
	logger.WarnContext(context.Background(), "warn visible", "topic", "votes.raw")

	output := buf.String()
	if strings.Contains(output, "info hidden") {
		t.Fatalf("expected info log to be filtered, got %q", output)
	}
	if !strings.Contains(output, "warn visible") {
		t.Fatalf("expected warn log to be present, got %q", output)
	}
	if !strings.Contains(output, "\"service\":\"projector\"") {
		t.Fatalf("expected service field in log output, got %q", output)
	}
}

func TestConfigureProducesStructuredErrorLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := MustConfigure("projector", "debug", &buf)

	logger.ErrorContext(context.Background(), "snapshot publish error", "votingId", "v-42", "error", "broker unavailable")

	output := buf.String()
	for _, want := range []string{
		"\"level\":\"ERROR\"",
		"snapshot publish error",
		"\"votingId\":\"v-42\"",
		"\"error\":\"broker unavailable\"",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in log output, got %q", want, output)
		}
	}
}

package logutil

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

func Configure(serviceName, level string, writer io.Writer) (*slog.Logger, error) {
	parsedLevel, err := parseLevel(level)
	if err != nil {
		return nil, err
	}

	if writer == nil {
		writer = os.Stdout
	}

	logger := slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: parsedLevel})).With(
		"service", serviceName,
	)
	slog.SetDefault(logger)
	return logger, nil
}

func MustConfigure(serviceName, level string, writer io.Writer) *slog.Logger {
	logger, err := Configure(serviceName, level, writer)
	if err != nil {
		panic(err)
	}
	return logger
}

func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q", level)
	}
}

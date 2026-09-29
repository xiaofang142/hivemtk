package logger

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestInitLogger(t *testing.T) {
	InitLogger(LoggingConfig{Level: "info", Output: "stdout"})
	if inst == nil {
		t.Error("Expected logger instance to be initialized")
	}
}

func TestGetLogger(t *testing.T) {
	log := GetLogger()
	if log == nil {
		t.Error("Expected logger instance to be returned")
	}
}

func TestDebug(t *testing.T) {
	Debug("This is a debug message")
}

func TestInfo(t *testing.T) {
	Info("This is an info message")
}

func TestWarn(t *testing.T) {
	Warn("This is a warning message")
}

func TestError(t *testing.T) {
	testErr := errors.New("test error")
	Error(testErr, "This is an error message")
}

func TestInitLogger_File(t *testing.T) {
	path := "test_file.log"
	InitLogger(LoggingConfig{Level: "debug", Output: "both", File: path, MaxSizeMB: 1})
	Info("file writer smoke test")
	os.Remove(path)
	os.Remove(path + ".1")
}

func TestCtxTraceAndModule(t *testing.T) {
	ctx := WithTraceID(context.Background(), "trace-1")
	ctx = WithModule(ctx, "orchestrator")
	l := Ctx(ctx)
	if l == nil {
		t.Error("Ctx should return a logger")
	}
	Ctx(ctx).Info().Msg("traced log")
}

func TestTraceIDRoundTrip(t *testing.T) {
	ctx := WithTraceID(context.Background(), "")
	if TraceIDFromContext(ctx) == "" {
		t.Error("WithTraceID should auto-generate a trace id")
	}
}

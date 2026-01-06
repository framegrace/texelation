package main

import (
	"errors"
	"testing"

	clientrt "github.com/framegrace/texelation/internal/runtime/client"
)

func TestRunParsesFlagsAndInvokesClient(t *testing.T) {
	var captured clientrt.Options
	runClient = func(opts clientrt.Options) error {
		captured = opts
		return nil
	}
	defer func() { runClient = clientrt.Run }()

	args := []string{"-socket", "/tmp/custom.sock", "-reconnect", "-panic-log", "/tmp/panic.log"}
	if err := run(args); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if captured.Socket != "/tmp/custom.sock" || !captured.Reconnect || captured.PanicLog != "/tmp/panic.log" {
		t.Fatalf("unexpected options passed to client: %+v", captured)
	}
}

func TestRunPropagatesClientError(t *testing.T) {
	expected := errors.New("boom")
	runClient = func(opts clientrt.Options) error { return expected }
	defer func() { runClient = clientrt.Run }()

	if err := run(nil); !errors.Is(err, expected) {
		t.Fatalf("expected error %v, got %v", expected, err)
	}
}

func TestRunFlagParseFailure(t *testing.T) {
	runClient = func(opts clientrt.Options) error { return nil }
	defer func() { runClient = clientrt.Run }()

	if err := run([]string{"-unknown"}); err == nil {
		t.Fatalf("expected flag parse error")
	}
}

// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package zoomdebug

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLogf_DisabledIsNoOp(t *testing.T) {
	t.Setenv("TEXELATION_DEBUG_ZOOM", "")
	t.Setenv("TEXELATION_DEBUG_ZOOM_FILE", "")
	resetForTesting()
	Init("client")

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	Logf("should not appear")
	if buf.Len() != 0 {
		t.Fatalf("expected no output when disabled, got %q", buf.String())
	}
}

func TestLogf_FallbackToLogPrintf(t *testing.T) {
	t.Setenv("TEXELATION_DEBUG_ZOOM", "1")
	t.Setenv("TEXELATION_DEBUG_ZOOM_FILE", "")
	resetForTesting()
	Init("client")

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	Logf("hello %s", "world")
	got := buf.String()
	if !contains(got, "[zoom-debug client] hello world") {
		t.Fatalf("expected log.Printf path with role prefix, got %q", got)
	}
}

func TestLogf_FileOutputRespectsRole(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zoom.log")
	t.Setenv("TEXELATION_DEBUG_ZOOM", "1")
	t.Setenv("TEXELATION_DEBUG_ZOOM_FILE", path)
	resetForTesting()
	Init("server")

	Logf("from server: %d", 42)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	got := string(data)
	if !contains(got, "[zoom-debug server] from server: 42") {
		t.Fatalf("expected server-prefixed line in file, got %q", got)
	}
}

func TestInit_DoubleCallSameRoleIsNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zoom.log")
	t.Setenv("TEXELATION_DEBUG_ZOOM", "1")
	t.Setenv("TEXELATION_DEBUG_ZOOM_FILE", path)
	resetForTesting()

	Init("client")

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	Init("client") // second same-role call should not warn

	Logf("ok")
	if contains(buf.String(), "Init called with role") {
		t.Fatalf("did not expect overwrite warning on same-role second Init, got %q",
			buf.String())
	}
}

func TestInit_DoubleCallDifferentRoleWarnsAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zoom.log")
	t.Setenv("TEXELATION_DEBUG_ZOOM", "1")
	t.Setenv("TEXELATION_DEBUG_ZOOM_FILE", path)
	resetForTesting()

	Init("client")

	var warnBuf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&warnBuf)
	defer log.SetOutput(prev)

	Init("server") // should warn and overwrite

	if !contains(warnBuf.String(), `Init called with role="server"`) {
		t.Fatalf("expected overwrite warning, got %q", warnBuf.String())
	}

	Logf("after overwrite")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !contains(string(data), "[zoom-debug server] after overwrite") {
		t.Fatalf("expected new role applied, got %q", string(data))
	}
}

func TestLogf_ConcurrentWritesNoInterleave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zoom.log")
	t.Setenv("TEXELATION_DEBUG_ZOOM", "1")
	t.Setenv("TEXELATION_DEBUG_ZOOM_FILE", path)
	resetForTesting()
	Init("client")

	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			Logf("line=%d aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", i)
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := bytes.Count(data, []byte("\n"))
	if lines != n {
		t.Fatalf("expected %d lines, got %d", n, lines)
	}
}

func contains(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}

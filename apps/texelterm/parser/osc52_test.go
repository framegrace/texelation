// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestOSC52_SetClipboard(t *testing.T) {
	v := NewVTerm(80, 24)
	p := NewParser(v)

	// Track clipboard sets
	var setData []byte
	v.OnClipboardSet = func(data []byte) {
		setData = data
	}

	// Send OSC 52 to set clipboard to "Hello, World!"
	testData := "Hello, World!"
	encoded := base64.StdEncoding.EncodeToString([]byte(testData))
	sequence := "\x1b]52;c;" + encoded + "\x07"

	for _, r := range sequence {
		p.Parse(r)
	}

	if string(setData) != testData {
		t.Errorf("Expected clipboard data %q, got %q", testData, string(setData))
	}
}

func TestOSC52_QueryClipboard(t *testing.T) {
	v := NewVTerm(80, 24)
	p := NewParser(v)

	// Mock clipboard with test data
	testData := "Test clipboard content"
	v.OnClipboardGet = func() []byte {
		return []byte(testData)
	}

	// Track PTY writes (responses)
	var ptyOutput []byte
	v.WriteToPty = func(b []byte) {
		ptyOutput = append(ptyOutput, b...)
	}

	// Send OSC 52 query (? instead of base64 data)
	sequence := "\x1b]52;c;?\x07"
	for _, r := range sequence {
		p.Parse(r)
	}

	// Verify response format: ESC]52;c;<base64>ESC\
	response := string(ptyOutput)
	expectedPrefix := "\x1b]52;c;"
	expectedSuffix := "\x1b\\"

	if !strings.HasPrefix(response, expectedPrefix) {
		t.Errorf("Response missing prefix. Got: %q", response)
	}
	if !strings.HasSuffix(response, expectedSuffix) {
		t.Errorf("Response missing suffix. Got: %q", response)
	}

	// Extract and decode base64 payload
	payload := response[len(expectedPrefix) : len(response)-len(expectedSuffix)]
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("Failed to decode base64 response: %v", err)
	}

	if string(decoded) != testData {
		t.Errorf("Expected clipboard response %q, got %q", testData, string(decoded))
	}
}

func TestOSC52_EmptySelectionParam(t *testing.T) {
	v := NewVTerm(80, 24)
	p := NewParser(v)

	// Track clipboard sets
	var setData []byte
	v.OnClipboardSet = func(data []byte) {
		setData = data
	}

	// Empty selection parameter defaults to s0, but we only handle 'c' or empty
	// Empty should also work for clipboard
	testData := "Empty param test"
	encoded := base64.StdEncoding.EncodeToString([]byte(testData))
	sequence := "\x1b]52;;" + encoded + "\x07"

	for _, r := range sequence {
		p.Parse(r)
	}

	if string(setData) != testData {
		t.Errorf("Expected clipboard data %q, got %q", testData, string(setData))
	}
}

func TestOSC52_IgnoreNonClipboard(t *testing.T) {
	v := NewVTerm(80, 24)
	p := NewParser(v)

	// Track clipboard sets
	called := false
	v.OnClipboardSet = func(data []byte) {
		called = true
	}

	// Send OSC 52 with 'p' (primary selection) - should be ignored
	testData := "Should not set"
	encoded := base64.StdEncoding.EncodeToString([]byte(testData))
	sequence := "\x1b]52;p;" + encoded + "\x07"

	for _, r := range sequence {
		p.Parse(r)
	}

	if called {
		t.Error("OnClipboardSet should not be called for non-clipboard selections")
	}
}

func TestOSC52_QueryWithoutHandler(t *testing.T) {
	v := NewVTerm(80, 24)
	p := NewParser(v)

	// Track PTY writes
	var ptyOutput []byte
	v.WriteToPty = func(b []byte) {
		ptyOutput = append(ptyOutput, b...)
	}

	// Don't set OnClipboardGet - should handle gracefully
	sequence := "\x1b]52;c;?\x07"
	for _, r := range sequence {
		p.Parse(r)
	}

	// Should not crash, and should not write anything
	if len(ptyOutput) > 0 {
		t.Errorf("Expected no PTY output when OnClipboardGet is nil, got: %q", string(ptyOutput))
	}
}

func TestOSC52_SetWithoutHandler(t *testing.T) {
	v := NewVTerm(80, 24)
	p := NewParser(v)

	// Don't set OnClipboardSet - should handle gracefully
	testData := "Test"
	encoded := base64.StdEncoding.EncodeToString([]byte(testData))
	sequence := "\x1b]52;c;" + encoded + "\x07"

	// Should not crash
	for _, r := range sequence {
		p.Parse(r)
	}
}

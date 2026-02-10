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

func TestOSC52_QueryNormalizesLineEndings(t *testing.T) {
	v := NewVTerm(80, 24)
	p := NewParser(v)

	// Mock clipboard with CRLF line endings (Windows-style)
	testData := "Line 1\r\nLine 2\r\nLine 3"
	v.OnClipboardGet = func() []byte {
		return []byte(testData)
	}

	// Track PTY writes (responses)
	var ptyOutput []byte
	v.WriteToPty = func(b []byte) {
		ptyOutput = append(ptyOutput, b...)
	}

	// Send OSC 52 query
	sequence := "\x1b]52;c;?\x07"
	for _, r := range sequence {
		p.Parse(r)
	}

	// Extract and decode base64 payload
	response := string(ptyOutput)
	expectedPrefix := "\x1b]52;c;"
	expectedSuffix := "\x1b\\"
	payload := response[len(expectedPrefix) : len(response)-len(expectedSuffix)]
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("Failed to decode base64 response: %v", err)
	}

	// Verify CRLF was normalized to LF
	expected := "Line 1\nLine 2\nLine 3"
	if string(decoded) != expected {
		t.Errorf("Expected normalized line endings %q, got %q", expected, string(decoded))
	}

	// Ensure no CRLF sequences remain
	if strings.Contains(string(decoded), "\r\n") {
		t.Error("Response still contains CRLF sequences after normalization")
	}
}

func TestOSC52_QueryPreservesLoneCarriageReturn(t *testing.T) {
	v := NewVTerm(80, 24)
	p := NewParser(v)

	// Test that lone CR (not followed by LF) is preserved
	testData := "Line 1\rOverwrite\nLine 2"
	v.OnClipboardGet = func() []byte {
		return []byte(testData)
	}

	var ptyOutput []byte
	v.WriteToPty = func(b []byte) {
		ptyOutput = append(ptyOutput, b...)
	}

	sequence := "\x1b]52;c;?\x07"
	for _, r := range sequence {
		p.Parse(r)
	}

	response := string(ptyOutput)
	payload := response[len("\x1b]52;c;") : len(response)-len("\x1b\\")]
	decoded, _ := base64.StdEncoding.DecodeString(payload)

	// Lone CR should be preserved (it's not part of CRLF)
	if string(decoded) != testData {
		t.Errorf("Expected %q, got %q", testData, string(decoded))
	}
}

func TestNormalizeLineEndings(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "CRLF to LF",
			input:    "Line 1\r\nLine 2\r\nLine 3",
			expected: "Line 1\nLine 2\nLine 3",
		},
		{
			name:     "Mixed line endings",
			input:    "Line 1\r\nLine 2\nLine 3\r\nLine 4",
			expected: "Line 1\nLine 2\nLine 3\nLine 4",
		},
		{
			name:     "Lone CR preserved",
			input:    "Line 1\rOverwrite",
			expected: "Line 1\rOverwrite",
		},
		{
			name:     "Only LF unchanged",
			input:    "Line 1\nLine 2\nLine 3",
			expected: "Line 1\nLine 2\nLine 3",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "No line endings",
			input:    "Single line",
			expected: "Single line",
		},
		{
			name:     "CR at end",
			input:    "Line 1\r\nLine 2\r",
			expected: "Line 1\nLine 2\r",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeLineEndings([]byte(tt.input))
			if string(result) != tt.expected {
				t.Errorf("normalizeLineEndings(%q) = %q, want %q",
					tt.input, string(result), tt.expected)
			}
		})
	}
}

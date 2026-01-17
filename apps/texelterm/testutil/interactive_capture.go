// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/testutil/interactive_capture.go
// Summary: Interactive PTY capture for testing terminal applications that need input.

package testutil

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// InteractiveCapture captures output from an interactive terminal session.
type InteractiveCapture struct {
	width  int
	height int
	ptmx   *os.File
	cmd    *exec.Cmd
	output bytes.Buffer
	mu     sync.Mutex
	done   chan struct{}

	// pendingResponses tracks DSR responses we've sent that may be echoed
	pendingResponses [][]byte
	responseMu       sync.Mutex
}

// CaptureAction represents an action to perform during capture.
type CaptureAction struct {
	// SendInput sends raw bytes to the PTY
	SendInput []byte
	// SendText sends text (convenience wrapper)
	SendText string
	// Wait pauses for a duration
	Wait time.Duration
}

// NewInteractiveCapture starts an interactive capture session.
func NewInteractiveCapture(command string, args []string, width, height int) (*InteractiveCapture, error) {
	cmd := exec.Command(command, args...)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("COLUMNS=%d", width),
		fmt.Sprintf("LINES=%d", height),
		"TERM=xterm-256color",
	)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(height),
		Cols: uint16(width),
	})
	if err != nil {
		return nil, fmt.Errorf("start pty: %w", err)
	}

	// Put PTY in raw mode to disable echo. This prevents terminal responses
	// (like DSR replies) from being echoed back and captured as output.
	// We ignore the oldState since we don't need to restore it.
	_, err = term.MakeRaw(int(ptmx.Fd()))
	if err != nil {
		ptmx.Close()
		return nil, fmt.Errorf("make pty raw: %w", err)
	}

	ic := &InteractiveCapture{
		width:  width,
		height: height,
		ptmx:   ptmx,
		cmd:    cmd,
		done:   make(chan struct{}),
	}

	// Start reading output
	go ic.readLoop()

	return ic, nil
}

func (ic *InteractiveCapture) readLoop() {
	defer close(ic.done)
	buf := make([]byte, 4096)
	for {
		n, err := ic.ptmx.Read(buf)
		if n > 0 {
			data := buf[:n]

			// Check for and respond to DSR queries
			ic.handleDSR(data)

			// Filter out any echoed responses from the captured data
			filtered := ic.filterEchoedResponses(data)

			ic.mu.Lock()
			ic.output.Write(filtered)
			ic.mu.Unlock()
		}
		if err != nil {
			if err != io.EOF {
				// Log but don't fail
			}
			return
		}
	}
}

// filterEchoedResponses removes any responses we sent that got echoed back.
// The PTY may echo responses in caret notation (^[ for ESC) or raw.
func (ic *InteractiveCapture) filterEchoedResponses(data []byte) []byte {
	ic.responseMu.Lock()
	defer ic.responseMu.Unlock()

	if len(ic.pendingResponses) == 0 {
		return data
	}

	// Make a copy to avoid modifying the original
	result := make([]byte, len(data))
	copy(result, data)

	for i := 0; i < len(ic.pendingResponses); i++ {
		resp := ic.pendingResponses[i]

		// Check for raw response (e.g., \x1b[1;1R)
		if idx := bytes.Index(result, resp); idx >= 0 {
			result = append(result[:idx], result[idx+len(resp):]...)
			// Remove from pending
			ic.pendingResponses = append(ic.pendingResponses[:i], ic.pendingResponses[i+1:]...)
			i--
			continue
		}

		// Check for caret notation version (e.g., ^[[1;1R for \x1b[1;1R)
		caretVersion := escapeToCaretNotation(resp)
		if idx := bytes.Index(result, caretVersion); idx >= 0 {
			result = append(result[:idx], result[idx+len(caretVersion):]...)
			// Remove from pending
			ic.pendingResponses = append(ic.pendingResponses[:i], ic.pendingResponses[i+1:]...)
			i--
			continue
		}
	}

	return result
}

// escapeToCaretNotation converts escape sequences to caret notation.
// ESC (0x1B) becomes ^[ (0x5E 0x5B)
func escapeToCaretNotation(data []byte) []byte {
	var result []byte
	for _, b := range data {
		if b == 0x1b {
			result = append(result, '^', '[')
		} else {
			result = append(result, b)
		}
	}
	return result
}

// handleDSR checks for Device Status Report queries and responds.
// It writes responses directly to the PTY and tracks them for filtering.
func (ic *InteractiveCapture) handleDSR(data []byte) {
	// Look for ESC[6n (cursor position query) and ESC[c (device attributes query)
	for i := 0; i < len(data)-2; i++ {
		if data[i] == 0x1b && data[i+1] == '[' {
			j := i + 2
			// Skip any parameters
			for j < len(data) && ((data[j] >= '0' && data[j] <= '9') || data[j] == ';' || data[j] == '?' || data[j] == '>') {
				j++
			}
			if j < len(data) {
				var response []byte
				switch data[j] {
				case 'n':
					// DSR query - check if it's 6n (cursor position)
					param := string(data[i+2 : j])
					if param == "6" {
						// Respond with cursor position (1;1)
						response = []byte("\x1b[1;1R")
					}
				case 'c':
					// Device Attributes query - respond with basic VT100
					param := string(data[i+2 : j])
					if param == "" || param == "0" || param == ">" || param == ">0" {
						response = []byte("\x1b[?1;2c")
					}
				}
				if response != nil {
					// Track the response for filtering echoes
					ic.responseMu.Lock()
					ic.pendingResponses = append(ic.pendingResponses, response)
					ic.responseMu.Unlock()

					ic.ptmx.Write(response)
				}
			}
		}
	}
}

// SendInput writes bytes to the PTY.
func (ic *InteractiveCapture) SendInput(data []byte) error {
	_, err := ic.ptmx.Write(data)
	return err
}

// SendText writes text to the PTY.
func (ic *InteractiveCapture) SendText(text string) error {
	return ic.SendInput([]byte(text))
}

// SendEnter sends Enter key.
func (ic *InteractiveCapture) SendEnter() error {
	return ic.SendInput([]byte{'\r'})
}

// SendCtrlC sends Ctrl+C.
func (ic *InteractiveCapture) SendCtrlC() error {
	return ic.SendInput([]byte{0x03})
}

// RunActions executes a sequence of actions.
func (ic *InteractiveCapture) RunActions(actions []CaptureAction) error {
	for _, action := range actions {
		if action.Wait > 0 {
			time.Sleep(action.Wait)
		}
		if action.SendText != "" {
			if err := ic.SendText(action.SendText); err != nil {
				return err
			}
		}
		if len(action.SendInput) > 0 {
			if err := ic.SendInput(action.SendInput); err != nil {
				return err
			}
		}
	}
	return nil
}

// WaitForOutput waits until output contains the given string or timeout.
func (ic *InteractiveCapture) WaitForOutput(contains string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ic.mu.Lock()
		data := ic.output.String()
		ic.mu.Unlock()
		if bytes.Contains([]byte(data), []byte(contains)) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// GetOutput returns the current captured output.
func (ic *InteractiveCapture) GetOutput() []byte {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	result := make([]byte, ic.output.Len())
	copy(result, ic.output.Bytes())
	return result
}

// ToRecording converts the capture to a Recording for comparison.
func (ic *InteractiveCapture) ToRecording() *Recording {
	return &Recording{
		Metadata: RecordingMetadata{
			Width:       ic.width,
			Height:      ic.height,
			Description: "Interactive capture",
			Timestamp:   time.Now(),
		},
		Sequences: ic.GetOutput(),
	}
}

// Close terminates the capture session.
func (ic *InteractiveCapture) Close() error {
	// Send Ctrl+C to try graceful exit
	ic.SendCtrlC()
	time.Sleep(100 * time.Millisecond)

	// Kill the process
	if ic.cmd.Process != nil {
		ic.cmd.Process.Kill()
	}
	ic.ptmx.Close()

	// Wait for read loop to finish
	<-ic.done

	return nil
}

// CaptureInteractive runs a command with actions and returns a recording.
// This is a convenience function for common capture patterns.
func CaptureInteractive(command string, args []string, width, height int, actions []CaptureAction, totalTimeout time.Duration) (*Recording, error) {
	ic, err := NewInteractiveCapture(command, args, width, height)
	if err != nil {
		return nil, err
	}
	defer ic.Close()

	// Run actions
	if err := ic.RunActions(actions); err != nil {
		return nil, fmt.Errorf("run actions: %w", err)
	}

	// Wait for remaining output
	time.Sleep(totalTimeout)

	return ic.ToRecording(), nil
}

// ParseKeySequence converts a key description string to bytes.
// Supports: <Enter>, <Escape>, <Tab>, <Backspace>, <Ctrl-X> where X is a letter
func ParseKeySequence(keyDesc string) []byte {
	switch keyDesc {
	case "<Enter>", "<Return>", "<CR>":
		return []byte{'\r'}
	case "<Escape>", "<Esc>":
		return []byte{0x1b}
	case "<Tab>":
		return []byte{'\t'}
	case "<Backspace>", "<BS>":
		return []byte{0x7f}
	case "<Space>":
		return []byte{' '}
	case "<Up>":
		return []byte("\x1b[A")
	case "<Down>":
		return []byte("\x1b[B")
	case "<Right>":
		return []byte("\x1b[C")
	case "<Left>":
		return []byte("\x1b[D")
	}

	// Check for Ctrl sequences
	if len(keyDesc) > 6 && keyDesc[:6] == "<Ctrl-" && keyDesc[len(keyDesc)-1] == '>' {
		char := keyDesc[6]
		if char >= 'a' && char <= 'z' {
			return []byte{char - 'a' + 1}
		}
		if char >= 'A' && char <= 'Z' {
			return []byte{char - 'A' + 1}
		}
	}

	// Return as-is if not a special sequence
	return []byte(keyDesc)
}

// ParseInputString converts a string with embedded key sequences to bytes.
// Example: "hello<Enter>world" -> "hello\rworld"
func ParseInputString(input string) []byte {
	var result bytes.Buffer
	i := 0
	for i < len(input) {
		if input[i] == '<' {
			// Find closing >
			end := i + 1
			for end < len(input) && input[end] != '>' {
				end++
			}
			if end < len(input) {
				keySeq := input[i : end+1]
				result.Write(ParseKeySequence(keySeq))
				i = end + 1
				continue
			}
		}
		result.WriteByte(input[i])
		i++
	}
	return result.Bytes()
}

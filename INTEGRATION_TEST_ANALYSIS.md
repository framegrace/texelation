# Integration Test Analysis

## Summary

This document analyzes the current integration test suite and identifies issues that need to be fixed. Integration tests are critical for future networking changes.

## Current Test Inventory

### Unit Tests (Working)
- `internal/runtime/server/*_test.go` - Basic server component tests
- `internal/runtime/client/*_test.go` - Client runtime components (newly added)
- `texel/*_test.go` - Core desktop tests
- `protocol/*_test.go` - Protocol encoding/decoding

### Integration Tests (Some Failing)

#### 1. `integration_test.go::TestConnectionSendsDiffProcessesAckAndKeyEvents`
- **Status**: ✅ Should work (no build tag, runs with regular tests)
- **Purpose**: Tests full connection flow with diff sending, ACK processing, and key events
- **Uses**: `net.Pipe()` for in-memory connections

#### 2. `client_integration_test.go::TestClientResumeReceivesSnapshot`
- **Status**: ❌ FAILING - "io: read/write on closed pipe"
- **Purpose**: Tests client resume flow with snapshot delivery
- **Issue**: Client closes connection before server finishes sending messages
- **Root Cause**: Race condition - client closes after reading expected messages, but server still trying to send

#### 3. `offline_resume_mem_test.go::TestOfflineRetentionAndResumeWithMemConn`
- **Status**: ❌ HANGING - Timeout after 10s
- **Purpose**: Tests offline message retention and resume with MemConn
- **Issue**: Blocks in `resumeClientFlow` at `readMessageSkippingFocus` - waiting for message that never arrives
- **Root Cause**: Resume flow sends 2 ACKs at line 266-269, but only expects to read one response. The test logic doesn't match the actual protocol flow.

#### 4. `clipboard_theme_mem_test.go::TestClipboardAndThemeRoundTrip`
- **Status**: ✅ PASSES (shown passing in partial run)
- **Purpose**: Tests clipboard set/get and theme updates
- **Uses**: `testutil.NewMemPipe()` for in-memory connections

#### 5. `server_desktop_integration_test.go::TestServerDesktopIntegrationProducesDiffsAndHandlesKeys`
- **Status**: ❓ UNKNOWN (needs testing)
- **Purpose**: End-to-end test of server+desktop producing diffs and handling keys
- **Uses**: `net.Pipe()`

#### 6. `texel/desktop_integration_test.go` - Desktop Integration Tests
- **Status**: ✅ PASSES
- **Purpose**: Tests desktop engine behavior (splits, workspaces, key/mouse injection, clipboard)
- **Note**: No build tag, runs with regular tests

## Detailed Issue Analysis

### Issue 1: TestClientResumeReceivesSnapshot - Closed Pipe Error

**Stack trace location:**
```
client_integration_test.go:197: resumeClient.Close()
client_integration_test.go:201: resume server error: io: read/write on closed pipe
```

**Problem:**
The test closes the client connection immediately after reading the snapshot, but the server's `sendPending()` is still trying to send buffered diffs. This creates a race condition.

**Fix Options:**
1. Add proper shutdown coordination - client should send a disconnect message
2. Don't treat "closed pipe" as an error in the test - it's expected behavior
3. Add a small delay before closing to let server finish

**Recommended Fix:**
```go
// In client_integration_test.go around line 197:
resumeClient.Close()
select {
case err := <-resumeErr:
    // Accept both nil and closed pipe errors as success
    if err != nil && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
        t.Fatalf("resume server error: %v", err)
    }
case <-time.After(50 * time.Millisecond):
    t.Fatalf("resume server did not finish")
}
```

### Issue 2: TestOfflineRetentionAndResumeWithMemConn - Hanging

**Stack trace location:**
```
offline_resume_mem_test.go:250: hdr, payload, err := readMessageSkippingFocus(clientConn)
```

**Problem:**
The test is stuck in a loop waiting for messages. Looking at the flow:

1. Test sends resume request
2. Server sends snapshot
3. Server sends 2 pending buffer deltas (retention limit was 2)
4. Test reads messages and ACKs each one
5. Test tries to read more messages but blocks forever

**Root Cause:**
The loop at line 249-277 continues until `session.Pending(0)` is empty, but it sends TWO ACKs for each delta (lines 266-269), which might be causing the session to think there are more pending diffs than there are.

**Fix:**
The test logic needs to match the actual protocol. After resuming, the server will send:
- 1 TreeSnapshot
- N BufferDeltas (where N = number of pending diffs, capped by retention limit)

The client should:
- Read and decode snapshot
- Read each delta, ACK it, check if queue is empty
- Close connection when done

**Recommended Fix:**
```go
// In resumeClientFlow around line 249:
snapshotReceived := false
for {
    hdr, payload, err := readMessageSkippingFocus(clientConn)
    if err != nil {
        if err == io.EOF {
            break
        }
        t.Fatalf("resume read message: %v", err)
    }
    switch hdr.Type {
    case protocol.MsgTreeSnapshot:
        if _, err := protocol.DecodeTreeSnapshot(payload); err != nil {
            t.Fatalf("resume decode snapshot: %v", err)
        }
        snapshotReceived = true
    case protocol.MsgBufferDelta:
        if _, err := protocol.DecodeBufferDelta(payload); err != nil {
            t.Fatalf("resume decode delta: %v", err)
        }
        // Send ONE ACK per delta
        ackPayload, _ := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: hdr.Sequence})
        if err := protocol.WriteMessage(clientConn, protocol.Header{
            Version: protocol.Version,
            Type: protocol.MsgBufferAck,
            Flags: protocol.FlagChecksum,
            SessionID: session.ID()
        }, ackPayload); err != nil {
            t.Fatalf("resume write ack: %v", err)
        }

        // Check if we're done - give server a moment to update pending count
        time.Sleep(5 * time.Millisecond)
        if len(session.Pending(0)) == 0 {
            _ = clientConn.Close()
            break
        }
    default:
        continue // Skip other message types
    }

    if len(session.Pending(0)) == 0 && snapshotReceived {
        _ = clientConn.Close()
        break
    }
}
```

## Missing Integration Test Coverage

Based on the CLAUDE.md architecture notes about future networking changes, we should add:

### 1. Connection Timeout Tests
- Test connection read/write timeouts
- Test handshake timeout
- Test idle connection cleanup

### 2. Concurrent Connection Tests
- Multiple clients connecting simultaneously
- Rapid connect/disconnect cycles
- Session ID collision handling

### 3. Network Failure Simulation Tests
- Partial message writes
- Corrupted message handling
- Out-of-order message delivery (if applicable)

### 4. Large Data Transfer Tests
- Large buffer deltas
- Many panes with large buffers
- Clipboard with large data

### 5. Protocol Version Tests
- Client with older protocol version
- Client with newer protocol version
- Version negotiation edge cases

### 6. Resume Edge Cases
- Resume with no pending diffs
- Resume with retention limit exceeded
- Resume with expired session
- Multiple resume attempts

## Recommendations

### Immediate Actions
1. Fix `TestClientResumeReceivesSnapshot` - Handle closed pipe gracefully
2. Fix `TestOfflineRetentionAndResumeWithMemConn` - Correct ACK logic
3. Run all integration tests to confirm status
4. Update test documentation

### Short-term Actions
1. Add connection timeout tests
2. Add concurrent connection tests
3. Add protocol version mismatch tests
4. Create integration test helper utilities

### Long-term Actions
1. Set up integration test CI pipeline
2. Add performance benchmarks
3. Add stress tests (like texel-stress but automated)
4. Document test patterns for contributors

## Test Infrastructure

### Available Test Utilities
- `testutil.MemConn` - In-memory bidirectional connection with buffering
- `testutil.NewMemPipe()` - Creates a pair of connected MemConns
- `net.Pipe()` - Standard Go in-memory pipe (unbuffered, synchronous)
- Stub screen drivers - For testing without real terminal

### Test Patterns
1. **Unit tests**: Test individual components in isolation
2. **Integration tests**: Test component interactions with real protocol
3. **End-to-end tests**: Full server+client flow (texel-stress)

### Build Tags
- Regular tests: No build tag (run with `go test`)
- Integration tests: `//go:build integration` (run with `go test -tags=integration`)
- Performance tests: Could add `//go:build performance` tag

## Next Steps

1. Create fixes for the two failing tests
2. Add new test cases for missing coverage
3. Document test expectations and patterns
4. Consider extracting common test utilities

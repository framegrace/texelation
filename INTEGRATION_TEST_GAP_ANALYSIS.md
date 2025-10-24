# Integration Test Gap Analysis & Client Behavior Testing

## Executive Summary

This document identifies missing integration test coverage and proposes an approach for testing client behaviors (splits, removals, resizing) using a headless client architecture.

**Key Finding**: We can absolutely test client behaviors with the headless client! The infrastructure already exists - we just need to expand it.

---

## Current Test Coverage

### What's Well Tested ✅

1. **Protocol Flow**
   - Handshake (hello, welcome, connect)
   - Resume with session ID
   - Buffer delta encoding/decoding/ACK
   - Snapshot delivery
   - Offline retention with limits

2. **Server-Side Desktop Operations** (texel/desktop_integration_test.go)
   - Splits (horizontal/vertical)
   - Workspace switching
   - Status pane management
   - Key/mouse event injection
   - Clipboard operations
   - Theme updates

3. **Client-Side Buffer Management** (client/buffercache_test.go)
   - Delta application
   - Snapshot application
   - Pane pruning
   - Revision tracking
   - Stale delta rejection
   - Geometry-based sorting

4. **Server Runtime**
   - Connection lifecycle
   - Session management
   - Diff queueing when offline
   - Retention limits
   - Focus metrics

### Critical Gaps 🔴

#### 1. **Client-Server Integration for Desktop Operations**
Currently NO tests for:
- Client receiving TreeSnapshot after server split
- Client buffer cache updates when panes are added/removed
- Client handling of pane geometry changes during splits
- Client state synchronization after tree modifications
- Multiple clients seeing same splits/removals

#### 2. **Resize Flow**
- MsgResize sent from client to server
- Server resizing desktop and all panes
- TreeSnapshot + BufferDeltas sent back to client
- Client applying new geometry

#### 3. **Multi-Client Scenarios**
- Two clients connected, one triggers split
- Second client receives TreeSnapshot
- Both clients have synchronized pane tree
- Concurrent operations from multiple clients

#### 4. **Tree Structure Edge Cases**
- Deep nesting (split within split within split)
- Remove pane that causes tree rebalancing
- Active pane tracking across splits/removes
- Focus transfer when active pane removed

#### 5. **Large-Scale Operations**
- Many panes (10+) with splits
- Large buffer deltas (1000+ rows)
- Many clients (5+) connected simultaneously
- Rapid split/close cycles

#### 6. **Error Conditions**
- Connection timeouts during split operation
- Client disconnects mid-TreeSnapshot
- Corrupted TreeSnapshot message
- Protocol version mismatch during handshake

---

## Headless Client Testing Architecture

### Current Capabilities

The headless client (`client/cmd/texel-headless/main.go`) already has:

1. **BufferCache** - Maintains local copy of all pane buffers
2. **TreeSnapshot handling** - Applies geometry updates
3. **BufferDelta handling** - Applies incremental updates
4. **PaneState handling** - Tracks active/resizing/z-order flags
5. **Resize sending** - Can trigger server resize operations

### What's Missing for Testing

The headless client needs:

1. **Test Harness Mode** - Accept commands via stdin or channel
2. **State Inspection API** - Query current pane tree structure
3. **Action Injection** - Send key events to trigger splits/closes
4. **Assertion Helpers** - Verify expected tree structure

---

## Proposed Testing Approach

### Architecture: Test Client with Action Queue

```go
type TestClient struct {
    conn         net.Conn
    cache        *client.BufferCache
    sessionID    [16]byte
    lastSequence uint64

    // Test control
    actions      chan TestAction
    assertions   chan TestAssertion
    eventLog     []TestEvent
}

type TestAction interface {
    Execute(tc *TestClient) error
}

// Examples:
// - SendKeyAction{Key: tcell.KeyCtrlB, Rune: 'h'}  // Split horizontal
// - SendKeyAction{Key: tcell.KeyCtrlB, Rune: 'x'}  // Close pane
// - SendResizeAction{Cols: 120, Rows: 40}
// - WaitForSnapshotAction{}
// - AssertPaneCountAction{Expected: 2}
```

### Test Pattern Example

```go
func TestClientReceivesTreeSnapshotAfterSplit(t *testing.T) {
    // Setup: Server + Desktop + Single Client
    server, desktop := setupServerWithDesktop(t, 80, 24)
    defer server.Close()

    client := NewTestClient(t, server.SocketPath())
    defer client.Close()

    // Initial state: 1 pane
    client.WaitForInitialSnapshot()
    client.AssertPaneCount(1)

    // Send Ctrl+B h (horizontal split)
    client.SendKey(tcell.KeyCtrlB, 'h', tcell.ModNone)

    // Client should receive TreeSnapshot with 2 panes
    snapshot := client.WaitForTreeSnapshot(500 * time.Millisecond)
    if len(snapshot.Panes) != 2 {
        t.Fatalf("expected 2 panes after split, got %d", len(snapshot.Panes))
    }

    // Verify geometry: both panes should be side-by-side
    left := snapshot.Panes[0]
    right := snapshot.Panes[1]

    if left.Width + right.Width > 80 {
        t.Fatalf("panes wider than screen: %d + %d > 80", left.Width, right.Width)
    }

    // Client cache should have 2 panes
    client.AssertPaneCount(2)

    // Both panes should receive buffer deltas
    client.WaitForBufferDelta(left.PaneID, 1*time.Second)
    client.WaitForBufferDelta(right.PaneID, 1*time.Second)
}
```

### Multi-Client Test Example

```go
func TestMultipleClientsReceiveSplit(t *testing.T) {
    server, desktop := setupServerWithDesktop(t, 80, 24)
    defer server.Close()

    client1 := NewTestClient(t, server.SocketPath())
    defer client1.Close()

    client2 := NewTestClient(t, server.SocketPath())
    defer client2.Close()

    // Both clients see initial state
    client1.WaitForInitialSnapshot()
    client2.WaitForInitialSnapshot()

    // Client 1 triggers split
    client1.SendKey(tcell.KeyCtrlB, 'h', tcell.ModNone)

    // Both clients should receive TreeSnapshot
    snapshot1 := client1.WaitForTreeSnapshot(500 * time.Millisecond)
    snapshot2 := client2.WaitForTreeSnapshot(500 * time.Millisecond)

    // Verify both see same tree structure
    if len(snapshot1.Panes) != len(snapshot2.Panes) {
        t.Fatalf("clients see different pane counts: %d vs %d",
            len(snapshot1.Panes), len(snapshot2.Panes))
    }

    // Verify pane IDs match
    for i := range snapshot1.Panes {
        if snapshot1.Panes[i].PaneID != snapshot2.Panes[i].PaneID {
            t.Fatalf("pane %d ID mismatch", i)
        }
    }
}
```

### Resize Test Example

```go
func TestClientResizeUpdatesTree(t *testing.T) {
    server, desktop := setupServerWithDesktop(t, 80, 24)
    defer server.Close()

    client := NewTestClient(t, server.SocketPath())
    defer client.Close()

    client.WaitForInitialSnapshot()

    // Trigger split
    client.SendKey(tcell.KeyCtrlB, 'h', tcell.ModNone)
    client.WaitForTreeSnapshot(500 * time.Millisecond)
    client.AssertPaneCount(2)

    // Send resize to 120x40
    client.SendResize(120, 40)

    // Client should receive new TreeSnapshot with updated geometry
    snapshot := client.WaitForTreeSnapshot(500 * time.Millisecond)

    totalWidth := 0
    maxHeight := 0
    for _, pane := range snapshot.Panes {
        if pane.Height > maxHeight {
            maxHeight = int(pane.Height)
        }
        // For horizontal split, panes are side-by-side
        totalWidth += int(pane.Width)
    }

    if maxHeight != 40 {
        t.Fatalf("expected pane height 40, got %d", maxHeight)
    }
    if totalWidth > 120 {
        t.Fatalf("total pane width exceeds 120: %d", totalWidth)
    }
}
```

---

## Implementation Plan

### Phase 1: Test Client Infrastructure (2-3 hours)

**File**: `internal/runtime/server/testutil/test_client.go`

```go
package testutil

type TestClient struct {
    conn          net.Conn
    cache         *client.BufferCache
    sessionID     [16]byte
    lastSequence  uint64

    snapshots     chan protocol.TreeSnapshot
    deltas        chan protocol.BufferDelta
    errors        chan error

    writeMu       sync.Mutex
    stopCh        chan struct{}
}

func NewTestClient(t *testing.T, socketPath string) *TestClient
func (tc *TestClient) SendKey(key tcell.Key, ch rune, mod tcell.ModMask) error
func (tc *TestClient) SendResize(cols, rows int) error
func (tc *TestClient) WaitForTreeSnapshot(timeout time.Duration) protocol.TreeSnapshot
func (tc *TestClient) WaitForBufferDelta(paneID [16]byte, timeout time.Duration) protocol.BufferDelta
func (tc *TestClient) AssertPaneCount(expected int) error
func (tc *TestClient) GetPaneGeometry(paneID [16]byte) (x, y, w, h int, err error)
func (tc *TestClient) Close() error
```

### Phase 2: Basic Tree Operation Tests (2-3 hours)

**File**: `internal/runtime/server/client_tree_integration_test.go`

Tests to implement:
1. `TestClientReceivesTreeSnapshotAfterHorizontalSplit`
2. `TestClientReceivesTreeSnapshotAfterVerticalSplit`
3. `TestClientReceivesTreeSnapshotAfterPaneClose`
4. `TestClientReceivesBufferDeltasForNewPanes`
5. `TestClientCacheUpdateAfterTreeChange`

### Phase 3: Multi-Client Tests (1-2 hours)

**File**: `internal/runtime/server/multi_client_integration_test.go`

Tests to implement:
1. `TestTwoClientsReceiveSameSplit`
2. `TestThreeClientsSeeSynchronizedTree`
3. `TestClientJoinsAfterSplitReceivesCorrectSnapshot`
4. `TestConcurrentSplitsFromDifferentClients`

### Phase 4: Resize Tests (1 hour)

**File**: `internal/runtime/server/resize_integration_test.go`

Tests to implement:
1. `TestClientResizeSinglePane`
2. `TestClientResizeAfterSplit`
3. `TestMultipleClientsWithDifferentSizes`

### Phase 5: Edge Cases (2-3 hours)

**File**: `internal/runtime/server/tree_edge_cases_test.go`

Tests to implement:
1. `TestDeepNestedSplits` (5+ levels)
2. `TestRemovePaneWithChildrenRebalances`
3. `TestActivePaneTrackingAcrossSplits`
4. `TestFocusTransferWhenActivePaneRemoved`
5. `TestRapidSplitCloseLoop`

### Phase 6: Error Handling (1-2 hours)

**File**: `internal/runtime/server/client_error_integration_test.go`

Tests to implement:
1. `TestClientDisconnectDuringTreeSnapshot`
2. `TestCorruptedTreeSnapshotMessage`
3. `TestConnectionTimeoutDuringSplit`

---

## Expected Benefits

### Immediate
1. **Confidence** in tree synchronization across client/server
2. **Coverage** of critical user workflows (split, close, resize)
3. **Regression protection** for future protocol changes

### Medium-term
1. **Documentation** - Tests serve as examples of expected behavior
2. **Debugging** - Easier to reproduce client/server sync issues
3. **Performance** - Can measure snapshot/delta overhead

### Long-term
1. **Protocol evolution** - Tests ensure backward compatibility
2. **Multi-client features** - Foundation for collaborative features
3. **Network resilience** - Can simulate network conditions

---

## Effort Estimate

| Phase | Effort | Risk |
|-------|--------|------|
| 1. Test Client Infrastructure | 2-3 hours | Low - Straightforward wrapper |
| 2. Basic Tree Tests | 2-3 hours | Low - Clear requirements |
| 3. Multi-Client Tests | 1-2 hours | Medium - Timing/sync complexity |
| 4. Resize Tests | 1 hour | Low - Straightforward |
| 5. Edge Cases | 2-3 hours | Medium - May uncover bugs |
| 6. Error Handling | 1-2 hours | Low - Controlled error injection |
| **Total** | **9-14 hours** | - |

---

## Recommendations

### Immediate (Do Now)
1. ✅ Implement TestClient infrastructure in `testutil/`
2. ✅ Write 5 basic tree operation tests
3. ✅ Verify tests catch real bugs (temporarily break split logic)

### Short-term (This Week)
1. Add multi-client tests
2. Add resize tests
3. Document test patterns for contributors

### Medium-term (This Month)
1. Add edge case tests
2. Add error handling tests
3. Create automated test reporting in CI

### Long-term (This Quarter)
1. Add performance benchmarks for tree operations
2. Add stress tests (100+ panes, 10+ clients)
3. Add protocol fuzzing tests

---

## Alternative Approaches Considered

### ❌ Approach 1: Test Without Real Client
- **Idea**: Just test desktop splits in isolation
- **Problem**: Doesn't verify protocol messages reach client correctly
- **Verdict**: Insufficient coverage

### ❌ Approach 2: Use tcell Client in Tests
- **Idea**: Run full tcell renderer in tests
- **Problem**: Requires terminal, slow, fragile
- **Verdict**: Too heavyweight for CI

### ✅ Approach 3: Headless Client (Chosen)
- **Idea**: Extend existing headless client for testing
- **Benefits**: Fast, no terminal needed, full protocol coverage
- **Trade-offs**: Need to implement test harness
- **Verdict**: Best balance of coverage and maintainability

---

## Conclusion

**The headless client architecture makes comprehensive client behavior testing feasible and practical.**

Key insights:
1. Infrastructure already exists (BufferCache, SimpleClient)
2. Just need thin test harness wrapper
3. Can test complex multi-client scenarios
4. ~10-14 hours of work for complete coverage
5. High return on investment

**Recommendation**: Proceed with Phase 1-2 immediately to establish foundation, then iterate based on findings.

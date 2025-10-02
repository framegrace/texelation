# Smoke & Integration Test Strategy

## Goals
- Provide fast confidence checks for desktop behaviour as the client/server split begins.
- Replace legacy `cmd/*` harnesses that previously exercised pane creation, tree persistence, and protocol flows.

## Proposed Suites
1. **Headless Desktop Smoke**
   - Entry: `go test ./texel/...` with the `_test.go` harness that instantiates the desktop using the new `ScreenDriver` stub.
   - Assertions: desktop initialisation, lifecycle wiring, and status pane attachment.

2. **Desktop Headless Lifecycle**
   - New tests in `texel/desktop_integration_test.go` cover workspace switching, pane splitting, and status-pane sizing using the stubbed screen driver.
   - Ensures core desktop invariants (active workspace, tree structure, area calculations) remain stable when refactoring.

3. **Session Persistence Check**
   - Unit tests for forthcoming persistence package once pane tree serialization exists; `go test ./server/persistence` will validate round-trips.
   - Include regression cases for empty tree, deep splits, and app-specific metadata.

4. **Protocol Loopback (Future Phase)**
   - Integration test that spins up server + client in-process over Unix sockets; asserts reconnection behaviour and diff replay.

## Tooling Hooks
- Extend `Makefile` with `smoke` target that sequentially runs the above suites (when implemented).
- Add GitHub Actions workflow once suites stabilize to ensure branches gate on smoke coverage.

## Action Items
- Implement mock tcell screen to unlock headless testing (Phase 1 deliverable).
- Draft initial buffer smoke CLI mirroring the old `cmd/full-test` behaviour but deterministic.
- Decide golden snapshot format (JSON vs. binary) before storing fixtures under version control.

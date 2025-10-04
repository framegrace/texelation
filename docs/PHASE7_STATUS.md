# Phase 7 Work Status (Persistence & Recovery)

## Goals
- Finalise snapshot format so it captures the pane tree geometry alongside buffers and titles.
- Restore a saved snapshot into a fresh desktop before accepting clients, providing immediate visuals during reconnects.
- Persist snapshots on interval and after structural changes with integrity checks for resumable sessions.

## Current Assessment
- Snapshot store writes/reads pane buffers and geometry with a hashed payload.
- Server caches the latest tree snapshot for outbound use but does not yet rehydrate the desktop on boot.
- Protocol snapshots currently omit tree structure, preventing faithful restore of splits and ratios.

## Next Steps
1. Extend the protocol/tree snapshot representation to capture split orientation, ratios, and leaf-pane references; update capture + decode paths and tests.
2. Introduce stable pane identifiers and placeholder snapshot apps so the desktop can render stored buffers while real apps spin up.
3. Apply boot snapshots to the desktop prior to listening and ensure persistence hooks run after workspace mutations.

---
_Last updated: 2025-10-04_

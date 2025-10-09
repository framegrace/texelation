# Phase 8 Work Status (Performance Tuning & Hardening)

## Goals
- Profile server/client paths under frequent updates; baseline CPU, allocations, and latency.
- Build benchmarks for protocol encode/decode, diff application, and renderer draws to catch regressions.
- Stress connection/resume workflows and ensure snapshot persistence keeps up without blocking publishers.
- Capture metrics (latency, throughput, queue depth) for dashboards; gate experimental protocol features behind flags.

## Current Assessment
- No sustained profiling runs yet; prior work focused on feature completion (Phase 7).
- Protocol encode/decode lacks dedicated benchmarks; existing coverage is functional-only.
- Snapshot persistence now carries tree + metadata, but no soak tests exercise repeated save/load cycles.
- Focus metrics plumbing exists but not yet wired into a central metrics sink.

## Next Steps
1. Instrument server publish pipeline with metrics and hooks for pprof capture; validate under synthetic load.
2. Add `go test -bench` suites for protocol encoding, buffer cache apply, and desktop capture/restore.
3. Create soak harness (possibly under `cmd/`) to simulate rapid diff production + resume loops and chart queue depth.
4. Evaluate allocator hotspots (e.g., string copies in tree snapshots) and experiment with pooling or delta compression toggles behind flags.

---
_Last updated: 2025-10-04_

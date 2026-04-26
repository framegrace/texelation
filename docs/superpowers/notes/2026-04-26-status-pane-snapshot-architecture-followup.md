# Status panes in the snapshot/restore path — architectural follow-up

**Status:** Note only. The current behavior in `texel/snapshot_restore.go` (filter status orphans by Title/AppType) is a tactical fix to unblock Plan D2 manual e2e. The proper fix is more invasive and worth a dedicated cycle.

## What's wrong with the current model

Status panes (the host's `desktop.AddStatusPane(...)` panes — typically the status bar) are second-class citizens in the snapshot/restore path:

1. **Capture** (`texel/snapshot.go captureStatusPaneSnapshots`) flattens status panes into the snapshot's `PaneSnapshot[]` alongside workspace panes, with `AltScreen: true` and a `Rect` derived from runtime layout.
2. **Storage** (`internal/runtime/server/snapshot_store.go StoredPane`) has no field that distinguishes a status pane from a regular workspace pane. Both round-trip identically.
3. **Restore** (`texel/snapshot_restore.go ApplyTreeCapture`) creates a regular `*pane` for every entry, including status panes, and passes them all through `PrepareAppForRestore` + `pendingAppStarts`.
4. **Boot wiring** (`cmd/texel-server/main.go`) re-adds the host's status panes via `desktop.AddStatusPane(...)` BEFORE `SetEventSink` triggers `applyBootCapture`. So at restore time, the host's status panes already exist in `d.statusPanes` — but their IDs are randomized at every boot (see `texel/desktop_engine_core.go newStatusPaneID`), so they have no link back to the snapshot's captured entries.

The result: the snapshot's captured status panes become orphans. They get a regular `*pane` with no tree home, get added to `pendingAppStarts`, and `StartPreparedApp` runs against a 0×0 `pane.Rect` (because no tree node ever sized them). The replay app exits cleanly, leaks a refresh notifier, and confuses the renderer (top/bottom borders missing on the workspace pane that ends up fighting for the same screen real estate).

## The current Plan D2 patch

`ApplyTreeCapture` filters by app `Title` (with an AppType fallback for `"statusbar"`) before adding to `pendingAppStarts`, and stops the orphan's app so it doesn't dangle. This works in practice for the single in-tree status app ("Status Bar") but is fragile:

- Title-based identity is unreliable as new status apps appear or get renamed.
- Workspace panes that happen to share a title with a registered status app would be incorrectly dropped.
- The orphan `*pane` is still allocated (and stays in the `panes` slice that `buildNodesFromCapture` indexes into) — it's just not started. Memory churn is small, but the data shape is misleading.

## Better designs to consider

### A. Separate field in `PaneSnapshot` / `StoredPane`

Add `IsStatusPane bool` (or `Kind StatusPaneSide` for richer routing). The capture path sets it for status panes; the restore path uses it to route those snapshots to a dedicated handler that:

- Looks up an existing status pane with matching `(side, app type)` and copies its captured `Buffer` for client-side replay.
- If no matching status pane exists, optionally creates one (for cases where a status app was added then removed).
- Never goes through `pendingAppStarts`.

Pros: explicit, robust, no string-matching. Cons: one schema bump on `StoredPane` (acceptable per project's "no back-compat" rule).

### B. Two-list snapshot

Split `StoredSnapshot.Panes` into `WorkspacePanes` and `StatusPanes`. Cleaner semantics; status panes never enter the workspace tree path.

Pros: structurally enforced — restore code can't accidentally route a status pane through the workspace path. Cons: bigger format change, more code touch.

### C. Stable status-pane IDs

Make `newStatusPaneID` deterministic from `(host, side, app type)` instead of random. Then the restore path can match captured IDs against runtime IDs and rehydrate the runtime status panes with their captured buffers (for client replay).

Pros: minimal code change. Cons: if a host adds two status apps of the same type/side, they collide. And the rehydration semantics get fuzzy — does the captured buffer overwrite the runtime app's first-render? Probably yes for cosmetic continuity.

### D. Don't capture status panes at all

The buffer-replay value of a captured statusbar is small — once the runtime statusbar app renders its first frame, the snapshot's captured rows are stale and overwritten. If we just stop capturing them, the snapshot/restore mismatch goes away entirely.

Pros: simplest possible. Cons: clients that resume on a daemon-restart see one frame of "no statusbar" before the runtime app renders. Acceptable cost for the simpler model.

## Recommendation

Start with **D** (stop capturing) as a single-commit cleanup. If a need for buffer-replay continuity emerges (e.g. status indicators that take seconds to compute and the user wants the previous frame visible during boot), revisit with **A** (explicit field).

Either way, remove the title-based filter once a proper mechanism is in place.

## Related context

- D2 commit `d474744` ("Plan D2: filter status orphans by Title/AppType") is the tactical fix this note replaces.
- The architectural concern was raised in user feedback during D2 manual e2e: "this treatment of the 'special' apps like status panel with a filter... is sort of pedestrian. Maybe we can architect something more proper."
- Touchpoints to coordinate when the proper fix lands: `texel/snapshot.go captureStatusPaneSnapshots`, `texel/snapshot_restore.go ApplyTreeCapture`, `internal/runtime/server/snapshot_store.go StoredPane`, `texel/desktop_status.go AddStatusPane`, `cmd/texel-server/main.go` boot wiring.

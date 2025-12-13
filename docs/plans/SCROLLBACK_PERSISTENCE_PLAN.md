# Scrollback Persistence

**Status**: Superseded by SCROLLBACK_REFLOW_PLAN.md

The three-level scrollback architecture (Disk → Memory → Display) was implemented as part of the scrollback reflow feature. See `docs/plans/SCROLLBACK_REFLOW_PLAN.md` for the complete architecture and implementation details.

## Summary

The scrollback system now uses:

1. **Disk History** (`parser/disk_history.go`) - TXHIST02 indexed format for unlimited persistent storage
2. **Scrollback History** (`parser/scrollback_history.go`) - ~5000 lines in-memory sliding window
3. **Display Buffer** (`parser/display_buffer.go`) - Physical lines wrapped to current terminal width

Lines are written to disk incrementally as they're committed. On scroll, lines are loaded from disk on-demand.

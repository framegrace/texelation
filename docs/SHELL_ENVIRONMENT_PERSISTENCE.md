# Shell Environment Persistence

## Overview

This document describes the shell environment persistence system in texelterm, which preserves environment variables across shell restarts and (planned) server restarts.

## Current Implementation (Phase 1: Shell Restart)

### File-Based Environment Capture

When a user declines to exit a shell (presses 'n' on the exit confirmation), the environment is restored from a temporary file.

**Components:**

1. **Shell Integration** (`~/.config/texelation/shell-integration/bash.sh`)
   - Runs after each command via `PROMPT_COMMAND`
   - Writes environment to `~/.texel-env.$$` using `env >| ~/.texel-env.$$`
   - Uses `>|` to bypass noclobber protection on Fedora
   - Skips first prompt to avoid capturing startup environment

2. **Terminal App** (`apps/texelterm/term.go:runShell()`)
   - On shell restart (after user declines exit), reads `~/.texel-env.<old-pid>`
   - Filters out `BASH_FUNC_*` entries (bash function exports that cause import errors)
   - Passes environment to new shell via `cmd.Env`
   - Deletes the temporary file after reading

3. **OSC 133 Integration**
   - Tracks command boundaries: A=prompt start, B=prompt end, C=command start, D=command end
   - Environment is captured after D (command end) marker

### Why File-Based?

Initial experiments with DCS (Device Control String) sequences failed due to fundamental bash limitations:

- DCS requires writing to stdout (the PTY)
- Base64-encoded environment is 8KB+ of data
- Bash's `PROMPT_COMMAND` waits for stdout-writing commands to complete
- Even background jobs with stdout connected cause bash to wait
- Result: prompt hangs until DCS transmission completes

File-based approach:
- `env >| file` is a fast file redirect with no pipes
- Completes instantly before prompt is shown
- No stdout writes that block the prompt
- Works reliably with noclobber, no hangs, no function import errors

## Future Implementation (Phase 2: Server Restart Persistence)

The file-based approach is temporary staging for the real persistence system.

### Architecture

Environment will be integrated into the terminal snapshot system:

```
1. Shell writes: env → ~/.texel-env.$$
2. Terminal periodically reads file → internal state
3. Snapshot system: terminal state (with env) → disk
4. Server restart: snapshot → restore terminal + environment
```

### Implementation Steps

1. **Terminal State Extension**
   - Add `lastEnvironment map[string]string` to terminal app state
   - Periodically poll/read latest `~/.texel-env.$$` file
   - Store parsed environment in memory

2. **Snapshot Integration**
   - Extend terminal snapshot format to include environment
   - Serialize environment alongside history, cursor position, etc.
   - Ensure snapshot integrity (environment can be large)

3. **Restore on Server Start**
   - When recreating terminal from snapshot, extract environment
   - Pass to shell startup via `cmd.Env` (same as current shell restart)
   - Delete snapshot file entry after successful restore

4. **Cleanup**
   - Remove debug logging from DCS handler (parser.go:239, 244)
   - Document the file-based approach as the correct solution
   - Consider periodic cleanup of orphaned `~/.texel-env.*` files

### Benefits

- **Full Persistence**: Layout + history + environment + apps = complete state restoration
- **Server Upgrades**: Stop server, upgrade binary, restart → users see no difference
- **Crash Recovery**: Server crash → restart → all terminals restored exactly
- **Session Management**: Save/load named sessions with full environment state

## Technical Details

### Environment File Format

Plain text, one `VAR=value` per line, same as `env` command output:

```bash
HOME=/home/marc
PATH=/usr/bin:/bin
TEST1=Blah
```

### Filtered Variables

- `BASH_FUNC_*%%`: Bash function exports (cause import errors)
- Future: Filter sensitive variables (API keys, tokens) before snapshot

### Performance

- File write: < 1ms (fast redirect, no pipes)
- File read: < 5ms (parse ~100 variables)
- Negligible impact on prompt latency

## Per-Terminal History Isolation

Each terminal panel maintains its own independent command history:

**Implementation:**
- Server passes pane ID to app via `PaneIDSetter` interface
- TexelTerm stores pane ID and sets `TEXEL_PANE_ID` environment variable
- Shell integration reads `$TEXEL_PANE_ID` and sets `HISTFILE=~/.texel-history-$TEXEL_PANE_ID`
- Each panel gets isolated history: `~/.texel-history-<pane-id-hex>`

**Benefits:**
- Multiple bash shells run simultaneously with independent histories
- No "last shell wins" problem when exiting shells
- History persists across shell restart (when declining exit confirmation)
- Each panel's history is preserved separately

## Testing

Current testing done:
- ✅ Environment preserved across shell restart
- ✅ Works with Fedora noclobber protection
- ✅ No bash function import errors
- ✅ No prompt hangs or delays
- ✅ Temporary files cleaned up after use
- ✅ Per-terminal history isolation implemented
- ✅ Pane ID passed to terminal apps via SetPaneID interface

Future testing needed:
- [ ] Manual verification of per-terminal history isolation
- [ ] Snapshot serialization/deserialization
- [ ] Server restart with multiple terminals
- [ ] Large environments (1000+ variables)
- [ ] Concurrent terminal startup from snapshots

## Related Files

- `~/.config/texelation/shell-integration/bash.sh` - Environment capture
- `apps/texelterm/term.go` - Environment restoration (runShell)
- `apps/texelterm/parser/parser.go` - OSC 133 integration, DCS handler
- `apps/texelterm/parser/vterm.go` - Command tracking callbacks

## Notes

- The DCS investigation was valuable - confirmed file-based is the right approach
- Environment files are PID-specific to avoid conflicts
- The file-based approach scales well to the snapshot system
- Consider extending to zsh, fish shells (separate integration scripts)

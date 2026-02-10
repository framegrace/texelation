# OSC 52 Clipboard Support

Texelterm implements OSC 52 (Operating System Command 52) for clipboard manipulation, allowing terminal applications like neovim to access the system clipboard.

## What is OSC 52?

OSC 52 is an escape sequence protocol defined in the XTerm specification that enables applications running inside the terminal to:
- **Set** the system clipboard with text/data
- **Query** the current clipboard contents

## Protocol Format

### Set Clipboard
```
OSC 52 ; Pc ; Pd ST
```
- `OSC` = `\x1b]` (ESC followed by `]`)
- `52` = Command number for clipboard manipulation
- `Pc` = Selection parameter:
  - `c` = clipboard (system clipboard)
  - `p` = primary selection (not currently supported)
  - `s` = select (not currently supported)
  - `0-7` = cut buffers (not currently supported)
  - Empty = defaults to `s0` but texelterm treats as clipboard
- `Pd` = Data:
  - Base64-encoded string to set clipboard
  - `?` to query clipboard
  - Empty to clear (not currently implemented)
- `ST` = String Terminator: `\x1b\\` (ESC-backslash) or `\x07` (BEL)

### Query Clipboard
```
OSC 52 ; c ; ? ST
```

The terminal responds with:
```
OSC 52 ; c ; <base64-encoded-clipboard-data> ST
```

## Usage in Applications

### Neovim

Neovim automatically detects OSC 52 support and uses it for clipboard operations when running in a remote terminal. Configure in `~/.config/nvim/init.vim` or `init.lua`:

```vim
" Enable OSC 52 clipboard
set clipboard+=unnamedplus
```

Or in Lua:
```lua
vim.opt.clipboard:append("unnamedplus")
```

Now you can:
- Yank to system clipboard: `"+y` or just `y` (if unnamed is set)
- Paste from system clipboard: `"+p` or `Ctrl-Shift-V`

### tmux

When running texelterm inside tmux, you may need to enable OSC 52 passthrough in `~/.tmux.conf`:

```tmux
set -g set-clipboard on
set -ag terminal-overrides "vte*:XT:Ms=\\E]52;c;%p2%s\\7"
```

### Testing OSC 52

You can test OSC 52 support manually:

```bash
# Set clipboard to "Hello, World!"
printf '\033]52;c;%s\a' $(echo -n "Hello, World!" | base64)

# Query clipboard (terminal will respond with OSC 52 sequence)
printf '\033]52;c;?\a'
```

## Implementation Details

### Architecture

1. **Parser** (`apps/texelterm/parser/parser.go`):
   - `handleOSC52()` - Parses OSC 52 sequences
   - Decodes base64 data for SET operations
   - Normalizes line endings (CRLF → LF) in GET responses
   - Triggers callbacks for GET/SET operations

2. **VTerm** (`apps/texelterm/parser/vterm.go`):
   - `OnClipboardSet(data []byte)` - Called when app sets clipboard
   - `OnClipboardGet() []byte` - Called when app queries clipboard
   - Configured via `WithClipboardSetHandler()` and `WithClipboardGetHandler()` options

3. **TexelTerm** (`apps/texelterm/term.go`):
   - Wires clipboard callbacks to desktop's `ClipboardService`
   - Handles both standalone and embedded modes
   - Updates callbacks on shell restart

### Clipboard Service Integration

Texelterm integrates with the `ClipboardService` interface from `texelui/core`:

```go
type ClipboardService interface {
    SetClipboard(mime string, data []byte)
    GetClipboard() (mime string, data []byte, ok bool)
}
```

- In **embedded mode** (running inside texelation), the desktop provides clipboard service
- In **standalone mode** (running directly), the runtime provides clipboard service
- The clipboard service abstracts system-specific clipboard access (X11, Wayland, macOS, Windows)

### Supported Operations

- ✅ Set clipboard via `OSC 52;c;<base64>ST`
- ✅ Query clipboard via `OSC 52;c;?ST`
- ✅ Line ending normalization (CRLF → LF) on query
- ✅ Empty selection parameter (defaults to clipboard)
- ❌ Primary selection (`p`) - ignored
- ❌ Cut buffers (`0-7`) - ignored
- ❌ Clear clipboard (empty `Pd`) - not implemented

### Line Ending Handling

When responding to OSC 52 clipboard queries, texelterm automatically normalizes line endings:
- **CRLF** (`\r\n`) → **LF** (`\n`)
- **Lone CR** (`\r`) → preserved (not part of CRLF)
- **LF** (`\n`) → unchanged

This prevents the "staircase effect" when pasting multi-line text in neovim and other terminal applications that expect Unix-style line endings.

## Security Considerations

OSC 52 allows applications to read and write the system clipboard, which could be a security concern:

1. **Write operations**: Applications can silently overwrite clipboard contents
2. **Read operations**: Applications can read sensitive data from clipboard (passwords, API keys, etc.)

Most modern terminals either:
- Require user confirmation for clipboard access (not implemented in texelterm)
- Disable OSC 52 by default (can be configured via `allowWindowOps` resource)
- Only allow clipboard writes, not reads

Currently texelterm allows both read and write without confirmation. Future enhancements could add:
- Configuration option to disable OSC 52
- User confirmation prompts for clipboard access
- Rate limiting to prevent clipboard spam

## Testing

Unit tests are in `apps/texelterm/parser/osc52_test.go`:

```bash
go test -v ./apps/texelterm/parser -run TestOSC52
```

Manual testing with neovim:
```bash
# Start texelterm
./bin/texelterm

# Inside texelterm, start neovim
nvim test.txt

# Try yanking and pasting
# 1. Type some text
# 2. Yank to clipboard: V (visual line) then "+y
# 3. Paste in another app (should work!)
# 4. Copy text from another app
# 5. Paste in neovim: "+p
```

## References

- [XTerm Control Sequences - OSC 52](http://invisible-island.net/xterm/ctlseqs/ctlseqs.html#h3-Operating-System-Commands)
- [tmux and OSC 52](https://github.com/tmux/tmux/wiki/Clipboard)
- [Neovim clipboard integration](https://neovim.io/doc/user/provider.html#clipboard-osc52)

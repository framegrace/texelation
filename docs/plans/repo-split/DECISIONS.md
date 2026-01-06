# Repository Split Decisions

Decisions made during planning session on 2026-01-04, updated after the TexelUI refactor.

## Key Decisions

### 1. GitHub Organization
**Decision**: Same user/org as current (`framegrace`)

### 2. Module Names
**Decision**: GitHub-based module paths
- TexelUI: `github.com/framegrace/texelui`
- Texelation: `github.com/framegrace/texelation`

### 3. Protocol Package Location
**Decision**: Keep in Texelation only

**Rationale**: Protocol is desktop-specific (pane snapshots, tree updates, client/server sync). Only Texelation needs it.

### 4. Event System
**Decision**: Both repos get event systems

**Details**:
- TexelUI: Gets `ControlBus` (simple trigger-based signaling)
- Texelation: Keeps full `EventDispatcher` with desktop-specific events (`EventPaneActiveChanged`, `EventTreeChanged`, etc.)

### 5. Git History Preservation
**Decision**: Use `git-filter-repo`

**Rationale**: Preserves commit history for files that move to each repo, while excluding unrelated commits.

### 6. Theme + Config Ownership
**Decision**: TexelUI owns the theme API; config files stay in `~/.config/texelation`.

**Details**:
- TexelUI loads themes from the shared `~/.config/texelation/theme.json`.
- Per-app theme overrides remain a Texelation-only feature.
- Theme defaults/palettes are shipped in both repos; if the theme file is missing, defaults are copied/saved.

### 7. Cards/Effects/Runtime Location
**Decision**: Cards, effects, and the devshell runner stay in Texelation.

**Rationale**: These are texel-app runtime concerns; TexelUI remains a pure TUI library.

### 8. TexelUI CLI + Demo
**Decision**: TexelUI CLI and demo move to the TexelUI repo.

**Details**:
- CLI (`texelui`) and bash adaptor are standalone, Texelation-independent.
- Demo runs as a normal Go app using TexelUI directly (no devshell).
- Runtime runner lives in TexelUI under `runtime/` (no Texelation devshell dependency).

### 9. Original Repository Fate
**Decision**: Archive/rename to texelation

**Details**:
- Current `tde` repo becomes/stays as `texelation`
- Files that move to texelui are removed
- Imports updated to use texelui as dependency

## Component Distribution

### Goes to TexelUI
- Core primitives: App, Cell, ControlBus, Storage interfaces
- Theme system
- Widget library (texelui/)
- TexelUI CLI + bash adaptor (texeluicli + cmd/texelui)
- TexelUI demo app (standalone)
- TexelUI docs (docs/texelui + TEXELUI_* guides)

### Stays in Texelation
- Desktop engine (Desktop, Pane, Tree, Workspace)
- EventDispatcher with desktop events
- StorageService implementation
- Buffer management
- Cards pipeline system
- Effects system
- Devshell runner (texel-app harness)
- All apps (texelterm, help, launcher, etc.)
- Config system + defaults
- Protocol
- Server/Client runtime
- App registry

## Future Considerations

- **texelterm split**: Could become its own repo later
- **Protocol library**: Could be extracted if needed for other projects
- **Effects library**: Could be extracted from both repos

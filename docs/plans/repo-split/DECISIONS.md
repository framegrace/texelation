# Repository Split Decisions

Decisions made during planning session on 2026-01-04.

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

### 6. Original Repository Fate
**Decision**: Archive/rename to texelation

**Details**:
- Current `tde` repo becomes/stays as `texelation`
- Files that move to texelui are removed
- Imports updated to use texelui as dependency

## Component Distribution

### Goes to TexelUI
- Core primitives: App, Cell, ControlBus, Storage interfaces
- Cards pipeline system
- Theme system
- Config system
- Widget library (texelui/)
- Standalone app runner (devshell)
- Default configuration files

### Stays in Texelation
- Desktop engine (Desktop, Pane, Tree, Workspace)
- EventDispatcher with desktop events
- StorageService implementation
- Buffer management
- All apps (texelterm, help, launcher, etc.)
- Protocol
- Server/Client runtime
- App registry

## Future Considerations

- **texelterm split**: Could become its own repo later
- **Protocol library**: Could be extracted if needed for other projects
- **Effects library**: Could be extracted from both repos

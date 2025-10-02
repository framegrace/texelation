# Repository Guidelines

## Project Structure & Module Organization
This repository implements Texelation, a Go desktop/terminal environment.
Core windowing and rendering logic lives in `texel/` (pane management, effects, themes).
Embeddable apps reside in `apps/` (e.g., `texelterm`, `statusbar`, `welcome`); launch wiring sits in `main.go`.
Command-line harnesses under `cmd/` provide integration fixtures (client/server shells, diagnostics).
Client transport helpers live in `client/`, while binary protocol definitions are in `protocol/`.
Generated assets or convenience binaries land in `bin/`; keep artefacts out of version control unless reproducible.

## Build, Test, and Development Commands
`go run .` starts the desktop with the default shell and welcome panes.
`go build ./cmd/texelation-server` produces the server binary; use matching `go run ./cmd/texelation-client` when testing remote flows.
`go test ./...` executes Go unit tests; add targeted `_test.go` files as you extend modules.
`go vet ./...` catches common Go anti-patterns prior to review.
`go run ./cmd/full-test` exercises the end-to-end UNIX-socket session and should pass before shipping networking changes.

## Coding Style & Naming Conventions
Use Go 1.24 toolchain and format with `gofmt` (tabs for indentation).
Follow standard Go naming: exported types/functions use CamelCase, private helpers stay lowerCamelCase, and avoid abbreviations not seen elsewhere in this repo.
Group package-level constants and vars logically, and keep files focused on a single responsibility (e.g., pane rendering, protocol message).
Update `texel/theme/` defaults alongside any new theme keys introduced in apps.

## Testing Guidelines
Prefer table-driven tests in `_test.go` files colocated with source.
When touching rendering or protocol logic, add integration coverage via the harnesses in `cmd/*test` and document any manual steps in the PR.
Aim for meaningful assertions rather than snapshot dumps; cover resize, split, and animation paths when modifying `texel/*`.
Capture regressions with reproducible scripts under `cmd/` rather than checking binaries into `bin/`.

## Commit & Pull Request Guidelines
Commit messages follow the short present-tense style seen in history (e.g., "Zoom working perfectly"); keep subject lines < 60 chars.
Scope each commit to one logical change and include context in the body when behaviour changes or migrations are needed.
PRs must describe motivation, testing performed (`go test`, integration harnesses, manual demos), and reference any tracking issues.
Attach screenshots or terminal recordings when UI/visual output changes, and call out impacts on theme configuration or sockets.

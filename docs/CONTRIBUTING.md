# Contributing to Texelation

This document captures the practical project guidelines that previously lived in
`AGENTS.md`. Everything here applies whether you are iterating on the desktop,
writing new apps, or hacking on the protocol.

---

## Repository Structure (Quick Tour)

| Path                          | Description |
|------------------------------|-------------|
| `texel/`                     | Core desktop engine (pane management, themes, effects plumbing). |
| `apps/`                      | Embeddable applications (texelterm, statusbar, welcome, clock). |
| `client/`                    | Remote client runtime and CLI wrappers. |
| `internal/runtime/server/`   | Server runtime (session manager, diff publisher, snapshot store). |
| `internal/effects/`          | Registry of workspace/pane effects shared by client and cards. |
| `texel/cards/`               | Card pipeline infrastructure for composing apps/effects. |
| `protocol/`                  | Binary protocol definitions, encoders/decoders. |
| `cmd/`                       | Command-line harnesses (server, stress tools, app-runner). |
| `docs/`                      | Architecture references, development guides, and testing plans. |
| `bin/`, `dist/`              | Build outputs (ignored in Git). |

---

## Build & Test Commands

* `make build` – Build `texel-server` and `texel-client` into `./bin`.
* `make install` – Install the binaries into `$GOPATH/bin`.
* `make release` – Cross-compile release artifacts into `./dist`.
* `make server` / `make client` – Run the server or client harness locally.
* `go test ./...` – Run unit tests across the module.
* `go test -tags=integration ./internal/runtime/server -run TestOfflineRetentionAndResumeWithMemConn` – Run the offline resume integration test.
* `go vet ./...` – Standard vet pass before code review.

The client/server architecture guide (`docs/CLIENT_SERVER_ARCHITECTURE.md`) has
additional operational details and smoke targets.

---

## Coding Style & Practices

* Go 1.24.x, formatted with `gofmt` (tabs for indentation).
* Exported names use CamelCase; keep unexported helpers in lowerCamelCase.
* Group related constants/vars; keep files focused on a single responsibility.
* Update theme defaults (`texel/theme/`) whenever adding new effect parameters.
* Prefer constructor helpers (`NewFoo(...)`) over exposing struct literals.

---

## Testing Guidelines

* Default to table-driven tests colocated with the code under test (`*_test.go`).
* When touching rendering or protocol logic, add coverage using the headless
  harnesses in `cmd/` or the memconn helpers in `internal/runtime/server/testutil`.
* Avoid snapshot diffs; assert on meaningful geometry/state changes.
* Capture regressions with scripts or sample buffers rather than checking binary
  assets into the repo.
* See `docs/SMOKE_TEST_PLAN.md` for the canonical smoke suites and future
  testing roadmap.

---

## Commit & Review Workflow

* Commit messages are short, present tense (e.g., “Fix resume ack loop”), and
  under ~60 characters.
* Keep each commit focused on one logical change; include context in the body
  if behaviour shifts noticeably.
* Always run `go test ./...` (and relevant integration tests) before pushing.
* Pull requests should document motivation, testing performed, and any manual
  validation. Include screenshots or gifs when altering UI behaviour or effects.

---

## Resources

* Architecture: `docs/CLIENT_SERVER_ARCHITECTURE.md`
* Effects development: `docs/EFFECTS_GUIDE.md`
* Building apps & card pipelines: `docs/TEXEL_APP_GUIDE.md`
* Future roadmap (architecture, testing, protocol): `docs/FUTURE_ROADMAP.md`

If you discover gaps in these docs while working on a feature, please update
them as part of your change. Keeping the written guidance fresh helps everyone.

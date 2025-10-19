#!/usr/bin/env python3
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]

DIR_INFO = {
    "client/cmd/texel-client": {
        "component": "remote client binary",
        "usage_template": "Invoked by end users to render the server-hosted desktop locally; other tooling wraps this runtime via internal/runtime/client.Run.",
        "notes": "Depends on the client runtime packages; keep it thin so alternate front-ends can reuse the same code.",
    },
    "client/cmd/texel-headless": {
        "component": "headless client harness",
        "usage_template": "Used in CI and automated tests to validate protocol flows without opening a tcell screen.",
        "notes": "Provides a minimal client for scripted scenarios.",
    },
    "client": {
        "component": "client runtime support library",
        "usage_template": "Imported by the remote renderer to manage {feature} during live sessions.",
        "notes": "Shared across multiple client binaries and tests.",
    },
    "apps/clock": {
        "component": "clock status widget",
        "usage_template": "Loaded into the desktop chrome to display time information.",
        "notes": "Uses shared texel primitives for drawing.",
    },
    "apps/statusbar": {
        "component": "status bar application",
        "usage_template": "Added to desktops to render workspace and mode metadata.",
        "notes": "Works in both local and remote deployments.",
    },
    "apps/texelterm/parser": {
        "component": "terminal parser module",
        "usage_template": "Consumed by the terminal app when decoding VT sequences.",
        "notes": "Keeps parsing concerns isolated from rendering.",
    },
    "apps/texelterm": {
        "component": "terminal application",
        "usage_template": "Spawned by desktop factories to provide shell access.",
        "notes": "Wraps PTY management and integrates with the parser package.",
    },
    "apps/welcome": {
        "component": "welcome application",
        "usage_template": "Presented on new sessions to guide users through the interface.",
        "notes": "Displays static content; simple example app.",
    },
    "internal/effects": {
        "component": "client effect subsystem",
        "usage_template": "Used by the client runtime to orchestrate {feature} visuals before rendering.",
        "notes": "Centralises every pane and workspace overlay so they can be configured via themes.",
    },
    "internal/runtime/client": {
        "component": "remote client runtime",
        "usage_template": "Embedded by client binaries to handle {feature} as part of the render/event loop.",
        "notes": "Owns session management, rendering, and protocol interaction for remote front-ends.",
    },
    "internal/runtime/server/testutil": {
        "component": "server runtime test utilities",
        "usage_template": "Imported by server tests when they need {feature} helpers.",
        "notes": "Not shipped with production binaries; only used in test code.",
    },
    "internal/runtime/server": {
        "component": "server runtime",
        "usage_template": "Used by texel-server to coordinate {feature} when hosting apps and sessions.",
        "notes": "This package bridges the legacy desktop code with the client/server protocol implementation.",
    },
    "protocol": {
        "component": "protocol definitions",
        "usage_template": "Shared by clients and servers to encode {feature} messages over the wire.",
        "notes": "Keep changes backward-compatible; any additions require coordinated version bumps.",
    },
    "texel/theme": {
        "component": "theme subsystem",
        "usage_template": "Accessed by both server and client when reading {feature} from theme configurations.",
        "notes": "Ensures user theme files always contain required defaults.",
    },
    "texel": {
        "component": "core desktop engine",
        "usage_template": "Used throughout the project to implement {feature} inside the desktop and panes.",
        "notes": "Legacy desktop logic migrated from the monolithic application.",
    },
    "cmd/texel-server": {
        "component": "server CLI harness",
        "usage_template": "Executed by operators to start the production server that manages sessions.",
        "notes": "Focuses on wiring flags and lifecycle around the internal runtime.",
    },
    "cmd/texel-stress": {
        "component": "stress harness",
        "usage_template": "Run in integration environments to pressure-test server throughput and protocol stability.",
        "notes": "Spawns multiple client connections to simulate load.",
    },
}

DEFAULT_INFO = {
    "component": "Texelation module",
    "usage_template": "Referenced within the project wherever {feature} support is required.",
    "notes": "Part of the Texelation AGPLv3 codebase.",
}


def choose_info(relative: str):
    # Select most specific directory info based on path prefix
    best_key = None
    for key in DIR_INFO:
        if relative.startswith(key + "/") or relative == key:
            if best_key is None or len(key) > len(best_key):
                best_key = key
    if best_key:
        return DIR_INFO[best_key]
    return DEFAULT_INFO


def humanise(name: str) -> str:
    words = name.replace('_', ' ').replace('-', ' ').split()
    if not words:
        return name
    return ' '.join(words)


def build_header(path: pathlib.Path, info: dict) -> list[str]:
    rel = path.relative_to(ROOT)
    base = path.stem
    component = info["component"]
    is_bench = base.endswith("_bench_test")
    is_test = base.endswith("_test") or base.endswith("_tests")
    words = []
    if is_bench:
        words = humanise(base[:-11])
    elif is_test:
        words = humanise(base[:-5]) if base.endswith("_test") else humanise(base)
    else:
        words = humanise(base)
    feature = words if isinstance(words, str) else words
    if isinstance(feature, str) and not feature:
        feature = "main"

    if is_bench:
        summary = f"Benchmarks {feature} performance within the {component}."
        usage = "Run via `go test -bench` to observe hot-path behaviour under load."
    elif is_test:
        summary = f"Exercises {feature} behaviour to ensure the {component} remains reliable."
        usage = "Executed during `go test` to guard against regressions."
    else:
        template = info["usage_template"]
        summary = f"Implements {feature} capabilities for the {component}."
        usage = template.format(feature=feature, component=component)

    notes = info.get("notes", DEFAULT_INFO["notes"])

    header = [
        "// Copyright © 2025 Texelation contributors",
        "// SPDX-License-Identifier: AGPL-3.0-or-later",
        "//",
        f"// File: {rel}",
        f"// Summary: {summary}",
        f"// Usage: {usage}",
        f"// Notes: {notes}",
    ]
    return header


def insert_header(path: pathlib.Path):
    text = path.read_text(encoding="utf-8")
    if "SPDX-License-Identifier" in text.splitlines()[:5]:
        return
    lines = text.splitlines()
    insert_idx = 0
    while insert_idx < len(lines) and lines[insert_idx].startswith("//go:build"):
        insert_idx += 1
    if insert_idx < len(lines) and lines[insert_idx].startswith("// +build"):
        insert_idx += 1
    while insert_idx < len(lines) and lines[insert_idx].strip() == "":
        insert_idx += 1

    info = choose_info(str(path.relative_to(ROOT)))
    header = build_header(path, info)
    new_lines = lines[:insert_idx] + header + [""] + lines[insert_idx:]
    path.write_text("\n".join(new_lines) + "\n", encoding="utf-8")


def main():
    modified = []
    for path in ROOT.rglob("*.go"):
        if "/vendor/" in str(path):
            continue
        insert_header(path)
        modified.append(path)
    print(f"Updated {len(modified)} Go files")


if __name__ == "__main__":
    sys.exit(main())

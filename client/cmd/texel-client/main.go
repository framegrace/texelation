// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: client/cmd/texel-client/main.go
// Summary: Implements main capabilities for the remote client binary.
// Usage: Invoked by end users to render the server-hosted desktop locally; other tooling wraps this runtime via internal/runtime/client.Run.
// Notes: Depends on the client runtime packages; keep it thin so alternate front-ends can reuse the same code.

package main

import (
	"flag"
	"log"

	clientrt "texelation/internal/runtime/client"
)

func main() {
	socket := flag.String("socket", "/tmp/texelation.sock", "Unix socket path")
	reconnect := flag.Bool("reconnect", false, "Attempt to resume previous session")
	panicLogPath := flag.String("panic-log", "", "File to append panic stack traces")
	flag.Parse()

	opts := clientrt.Options{
		Socket:    *socket,
		Reconnect: *reconnect,
		PanicLog:  *panicLogPath,
	}
	if err := clientrt.Run(opts); err != nil {
		log.Fatal(err)
	}
}

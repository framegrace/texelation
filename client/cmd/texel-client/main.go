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
	"io"
	"log"
	"os"

	clientrt "github.com/framegrace/texelation/internal/runtime/client"
)

var runClient = clientrt.Run

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("texel-client", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	socket := fs.String("socket", "/tmp/texelation.sock", "Unix socket path")
	reconnect := fs.Bool("reconnect", false, "Attempt to resume previous session")
	panicLogPath := fs.String("panic-log", "", "File to append panic stack traces")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts := clientrt.Options{
		Socket:    *socket,
		Reconnect: *reconnect,
		PanicLog:  *panicLogPath,
	}
	return runClient(opts)
}

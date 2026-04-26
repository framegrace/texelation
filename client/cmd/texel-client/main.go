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
	"fmt"
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
	clientName := fs.String("client-name", "", "Client identity slot for persistence (default: $TEXELATION_CLIENT_NAME or \"default\")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Plan D: validate --client-name (or $TEXELATION_CLIENT_NAME)
	// early. Either input expresses the user's intent to use a named
	// slot; silently disabling persistence later is a UX trap.
	// ValidateClientName checks only the name — never touches $HOME
	// or the socket — so failures unambiguously blame the right input.
	if *clientName != "" {
		if err := clientrt.ValidateClientName(*clientName); err != nil {
			return fmt.Errorf("invalid --client-name %q: %w", *clientName, err)
		}
		// Warn when the flag silently overrides a non-empty env var.
		if envName := os.Getenv(clientrt.ClientNameEnvVar); envName != "" && envName != *clientName {
			log.Printf("note: --client-name=%q overrides $%s=%q", *clientName, clientrt.ClientNameEnvVar, envName)
		}
	} else if envName := os.Getenv(clientrt.ClientNameEnvVar); envName != "" {
		if err := clientrt.ValidateClientName(envName); err != nil {
			return fmt.Errorf("invalid $%s %q: %w", clientrt.ClientNameEnvVar, envName, err)
		}
	}
	opts := clientrt.Options{
		Socket:     *socket,
		Reconnect:  *reconnect,
		PanicLog:   *panicLogPath,
		ClientName: *clientName,
	}
	return runClient(opts)
}

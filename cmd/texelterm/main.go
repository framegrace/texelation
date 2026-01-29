package main

import (
	"flag"
	"log"
	"strings"

	"github.com/framegrace/texelation/apps/texelterm"
	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/runtime"
	"github.com/gdamore/tcell/v2"
)

func main() {
	flag.Parse()

	builder := func(args []string) (texelcore.App, error) {
		shell := "/bin/bash"
		if len(args) > 0 {
			shell = strings.Join(args, " ")
		}
		return texelterm.New("texelterm", shell), nil
	}

	// Disable exit key - texelterm exits only when the shell exits
	opts := runtime.Options{ExitKey: tcell.Key(-1)}
	if err := runtime.RunWithOptions(builder, opts, flag.Args()...); err != nil {
		log.Fatalf("texelterm: %v", err)
	}
}

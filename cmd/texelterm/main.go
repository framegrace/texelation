package main

import (
	"flag"
	"log"
	"strings"

	"github.com/framegrace/texelation/apps/texelterm"
	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/runtime"
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

	if err := runtime.Run(builder, flag.Args()...); err != nil {
		log.Fatalf("texelterm: %v", err)
	}
}

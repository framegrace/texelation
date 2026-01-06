package main

import (
	"flag"
	"log"

	"github.com/framegrace/texelation/apps/configeditor"
	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/runtime"
)

func main() {
	flag.Parse()

	builder := func(_ []string) (texelcore.App, error) {
		return configeditor.New(nil), nil
	}

	if err := runtime.Run(builder, flag.Args()...); err != nil {
		log.Fatalf("config-editor: %v", err)
	}
}

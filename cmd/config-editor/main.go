package main

import (
	"flag"
	"log"

	"github.com/framegrace/texelation/internal/devshell"
)

func main() {
	flag.Parse()
	if err := devshell.RunApp("config-editor", flag.Args()); err != nil {
		log.Fatalf("config-editor: %v", err)
	}
}

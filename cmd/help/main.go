package main

import (
	"flag"
	"log"

	"github.com/framegrace/texelation/internal/devshell"
)

func main() {
	flag.Parse()
	if err := devshell.RunApp("help", flag.Args()); err != nil {
		log.Fatalf("help: %v", err)
	}
}

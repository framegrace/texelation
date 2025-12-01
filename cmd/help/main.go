package main

import (
	"flag"
	"log"

	"texelation/internal/devshell"
)

func main() {
	flag.Parse()
	if err := devshell.RunApp("welcome", flag.Args()); err != nil {
		log.Fatalf("welcome: %v", err)
	}
}

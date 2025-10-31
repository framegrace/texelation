package main

import (
	"flag"
	"log"

	"texelation/internal/devshell"
)

func main() {
	flag.Parse()
	if err := devshell.RunApp("texelterm", flag.Args()); err != nil {
		log.Fatalf("texelterm: %v", err)
	}
}

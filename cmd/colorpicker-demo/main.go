package main

import (
	"flag"
	"log"
	"texelation/internal/devshell"
)

func main() {
	flag.Parse()
	if err := devshell.RunApp("colorpicker-demo", flag.Args()); err != nil {
		log.Fatalf("colorpicker-demo: %v", err)
	}
}

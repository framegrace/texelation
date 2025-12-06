package main

import (
	"flag"
	"log"
	"texelation/internal/devshell"
)

func main() {
	flag.Parse()
	if err := devshell.RunApp("texelui-demo", flag.Args()); err != nil {
		log.Fatalf("texelui-demo: %v", err)
	}
}

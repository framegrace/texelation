package main

import (
	"flag"
	"log"
	"texelation/internal/devshell"
)

func main() {
	flag.Parse()
	if err := devshell.RunApp("texelui-demo2", flag.Args()); err != nil {
		log.Fatalf("texelui-demo2: %v", err)
	}
}

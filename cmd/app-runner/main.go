package main

import (
	"flag"
	"log"

	"github.com/framegrace/texelation/internal/devshell"
)

func main() {
	appName := flag.String("app", "", "name of the app to run (e.g., texelterm)")
	flag.Parse()
	if *appName == "" {
		log.Fatal("please specify -app")
	}
	if err := devshell.RunApp(*appName, flag.Args()); err != nil {
		log.Fatalf("run failed: %v", err)
	}
}

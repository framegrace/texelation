package main

import (
	"flag"
	"log"
	"strings"

	"github.com/framegrace/texelation/apps/texelbrowse"
	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/runtime"
)

func main() {
	flag.Parse()

	builder := func(args []string) (texelcore.App, error) {
		url := ""
		if len(args) > 0 {
			url = strings.Join(args, " ")
		}
		return texelbrowse.New(url), nil
	}

	if err := runtime.Run(builder, flag.Args()...); err != nil {
		log.Fatalf("texelbrowse: %v", err)
	}
}

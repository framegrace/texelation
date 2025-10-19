package main

import (
	"flag"
	"log"

	clientrt "texelation/internal/runtime/client"
)

func main() {
	socket := flag.String("socket", "/tmp/texelation.sock", "Unix socket path")
	reconnect := flag.Bool("reconnect", false, "Attempt to resume previous session")
	panicLogPath := flag.String("panic-log", "", "File to append panic stack traces")
	flag.Parse()

	opts := clientrt.Options{
		Socket:    *socket,
		Reconnect: *reconnect,
		PanicLog:  *panicLogPath,
	}
	if err := clientrt.Run(opts); err != nil {
		log.Fatal(err)
	}
}

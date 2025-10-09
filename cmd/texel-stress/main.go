package main

import (
    "context"
    "flag"
    "fmt"
    "time"
)

func main() {
    duration := flag.Duration("duration", 10*time.Second, "how long to run the stress test")
    flag.Parse()

    ctx, cancel := context.WithTimeout(context.Background(), *duration)
    defer cancel()

    <-ctx.Done()
    fmt.Println("stress run complete (placeholder)")
}

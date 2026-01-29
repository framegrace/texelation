package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/framegrace/texelation/apps/texelterm"
	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/runtime"
	"github.com/gdamore/tcell/v2"
)

var resetHistory = flag.Bool("reset-history", false, "Remove all scrollback history and search indexes")

func main() {
	flag.Parse()

	if *resetHistory {
		if err := handleResetHistory(); err != nil {
			log.Fatalf("texelterm: %v", err)
		}
		return
	}

	builder := func(args []string) (texelcore.App, error) {
		shell := "/bin/bash"
		if len(args) > 0 {
			shell = strings.Join(args, " ")
		}
		return texelterm.New("texelterm", shell), nil
	}

	// Disable exit key - texelterm exits only when the shell exits
	opts := runtime.Options{ExitKey: tcell.Key(-1)}
	if err := runtime.RunWithOptions(builder, opts, flag.Args()...); err != nil {
		log.Fatalf("texelterm: %v", err)
	}
}

func handleResetHistory() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	// Find all history directories (handle legacy double-scrollback path)
	basePath := filepath.Join(homeDir, ".texelation")
	var dirsToRemove []string
	var fileCount, totalSize int64

	// Check both possible scrollback locations
	possiblePaths := []string{
		filepath.Join(basePath, "scrollback"),
	}

	for _, dir := range possiblePaths {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirsToRemove = append(dirsToRemove, dir)
			filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					fileCount++
					totalSize += info.Size()
				}
				return nil
			})
		}
	}

	if len(dirsToRemove) == 0 {
		fmt.Println("No history found. Nothing to reset.")
		return nil
	}

	fmt.Printf("This will permanently delete all scrollback history:\n")
	for _, dir := range dirsToRemove {
		fmt.Printf("  Directory: %s\n", dir)
	}
	fmt.Printf("  Files: %d (%.2f MB)\n", fileCount, float64(totalSize)/(1024*1024))
	fmt.Printf("\nType 'yes' to confirm: ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response != "yes" {
		fmt.Println("Aborted.")
		return nil
	}

	// Remove all found directories
	for _, dir := range dirsToRemove {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("failed to remove %s: %w", dir, err)
		}
		fmt.Printf("Removed: %s\n", dir)
	}

	fmt.Println("History reset complete.")
	return nil
}

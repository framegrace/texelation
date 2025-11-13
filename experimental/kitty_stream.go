package main

import (
	"bufio"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	w := flag.Int("w", 1280, "frame width (px)")
	h := flag.Int("h", 720, "frame height (px)")
	pixfmt := flag.String("pixfmt", "rgbx", "rgbx or bgrx")
	stride := flag.Int("stride", 0, "bytes per row from source (0 = width*4)")
	chunk := flag.Int("chunk", 262144, "raw bytes per chunk before base64")
	id := flag.Int("id", 777, "kitty image id (persistent)")
	z := flag.Int("z", 999, "z-index (higher draws on top)")
	flag.Parse()

	const bpp = 4
	rowBytes := (*w) * bpp
	if *stride == 0 {
		*stride = rowBytes
	}
	srcFrame := (*stride) * (*h) // bytes read per frame
	dstFrame := rowBytes * (*h)  // tightly packed frame we send

	swapBGR := (*pixfmt == "bgrx")

	// Alt screen; clear screen+scrollback; home; hide cursor; no wrap
	write("\x1b[?1049h\x1b[2J\x1b[3J\x1b[H\x1b[?25l\x1b[?7l")

	// Start clean: delete any stale image with same id
	write(fmt.Sprintf("\x1b_Ga=d,i=%d,q=2\x1b\\", *id))

	// Place once at (0,0) and freeze cursor movement with C=1
	place := func() {
		write("\x1b[H")
		// a=p, x/y/X/Y at top-left, size s/v, z-index, C=1 (no cursor move)
		write(fmt.Sprintf("\x1b_Ga=p,i=%d,q=2,x=0,y=0,X=0,Y=0,s=%d,v=%d,z=%d,C=1\x1b\\",
			*id, *w, *h, *z))
	}
	place()

	// Clean exit restores TTY and removes the image
	cleanup := func() {
		write(fmt.Sprintf("\x1b_Ga=d,i=%d,q=2\x1b\\", *id))
		write("\x1b[?7h\x1b[?25h\x1b[?1049l")
		_ = os.Stdout.Sync()
	}
	defer cleanup()

	// Only re-place on resize; quit on other signals
	sigc := make(chan os.Signal, 4)
	signal.Notify(sigc, syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for s := range sigc {
			if s == syscall.SIGWINCH {
				place()
			} else {
				cleanup()
				os.Exit(0)
			}
		}
	}()

	// Prebuilt transmit headers (C=1 again, to enforce no-cursor-move on updates)
	header := func(more bool) string {
		if more {
			return fmt.Sprintf("\x1b_Ga=T,f=32,s=%d,v=%d,q=2,i=%d,m=1,C=1;", *w, *h, *id)
		}
		return fmt.Sprintf("\x1b_Ga=T,f=32,s=%d,v=%d,q=2,i=%d,m=0,C=1;", *w, *h, *id)
	}
	const footer = "\x1b\\"

	reader := bufio.NewReaderSize(os.Stdin, *chunk*4)
	src := make([]byte, srcFrame)
	dst := make([]byte, dstFrame)

	for {
		// Read one full source frame
		if _, err := io.ReadFull(reader, src); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return
			}
			fmt.Fprintf(os.Stderr, "read frame: %v\n", err)
			return
		}
		// Drop stale frames if multiple are queued
		for reader.Buffered() >= srcFrame {
			if _, err := io.ReadFull(reader, src); err != nil {
				break
			}
		}

		// Repack stride -> tight rows
		for y := 0; y < *h; y++ {
			copy(dst[y*rowBytes:(y+1)*rowBytes], src[y*(*stride):y*(*stride)+rowBytes])
		}
		if swapBGR {
			swapBGRXtoRGBX(dst)
		}

		// Synchronized output for atomic paint
		write("\x1b[?2026h")

		// Update pixels of the existing image (no delete/place)
		left := dst
		for len(left) > 0 {
			n := *chunk
			if n > len(left) {
				n = len(left)
			}
			part := left[:n]
			left = left[n:]

			write(header(len(left) > 0))
			enc := base64.NewEncoder(base64.StdEncoding, os.Stdout)
			_, _ = enc.Write(part)
			_ = enc.Close()
			write(footer)
		}

		write("\x1b[?2026l")
		_ = os.Stdout.Sync()
	}
}

func swapBGRXtoRGBX(p []byte) {
	for i := 0; i+3 < len(p); i += 4 {
		p[i+0], p[i+2] = p[i+2], p[i+0]
	}
}

func write(s string) { _, _ = os.Stdout.WriteString(s) }

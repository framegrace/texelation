package texeluicli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func SocketPath(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if env := os.Getenv("TEXELUI_SOCKET"); env != "" {
		return env, nil
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = os.TempDir()
	}
	dir := filepath.Join(runtimeDir, "texelui")
	uid := os.Getuid()
	return filepath.Join(dir, fmt.Sprintf("daemon-%d.sock", uid)), nil
}

func EnsureServer(socketPath string) error {
	if socketPath == "" {
		var err error
		socketPath, err = SocketPath("")
		if err != nil {
			return err
		}
	}
	conn, err := net.Dial("unix", socketPath)
	if err == nil {
		_ = conn.Close()
		return nil
	}
	_ = os.Remove(socketPath)
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--server", "--socket", socketPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("failed to start texelui server")
}

func SendRequest(req Request, socketPath string) (Response, error) {
	if socketPath == "" {
		var err error
		socketPath, err = SocketPath("")
		if err != nil {
			return Response{}, err
		}
	}
	if err := EnsureServer(socketPath); err != nil {
		return Response{}, err
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return Response{}, err
	}
	dec := json.NewDecoder(conn)
	var resp Response
	if err := dec.Decode(&resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}

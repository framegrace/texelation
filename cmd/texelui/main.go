package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"texelation/apps/texeluicli"
)

func main() {
	global := flag.NewFlagSet("texelui", flag.ExitOnError)
	serverMode := global.Bool("server", false, "run server daemon")
	socketPath := global.String("socket", "", "override socket path")
	_ = global.Parse(os.Args[1:])

	if *serverMode {
		path, err := texeluicli.SocketPath(*socketPath)
		if err != nil {
			exitError(err)
		}
		if err := texeluicli.RunServer(path); err != nil {
			exitError(err)
		}
		return
	}

	args := global.Args()
	if len(args) == 0 {
		usage()
		return
	}
	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "open":
		openCmd(cmdArgs, *socketPath)
	case "wait":
		waitCmd(cmdArgs, *socketPath)
	case "get":
		getCmd(cmdArgs, *socketPath)
	case "set":
		setCmd(cmdArgs, *socketPath)
	case "append":
		appendCmd(cmdArgs, *socketPath)
	case "run":
		runCmd(cmdArgs, *socketPath)
	case "close":
		closeCmd(cmdArgs, *socketPath)
	default:
		usage()
	}
}

func openCmd(args []string, socketPath string) {
	fs := flag.NewFlagSet("open", flag.ExitOnError)
	specPath := fs.String("spec", "-", "spec file path or - for stdin")
	_ = fs.Parse(args)

	var reader io.Reader
	if *specPath == "-" {
		reader = os.Stdin
	} else {
		f, err := os.Open(*specPath)
		if err != nil {
			exitError(err)
		}
		defer f.Close()
		reader = f
	}

	spec, err := texeluicli.DecodeSpec(reader)
	if err != nil {
		exitError(err)
	}
	req := texeluicli.Request{Cmd: "open", Spec: &spec}
	resp, err := texeluicli.SendRequest(req, socketPath)
	if err != nil {
		exitError(err)
	}
	if !resp.OK {
		exitError(fmt.Errorf(resp.Error))
	}
	fmt.Println(resp.Session)
}

func waitCmd(args []string, socketPath string) {
	fs := flag.NewFlagSet("wait", flag.ExitOnError)
	events := fs.String("events", "", "comma-separated event filters (e.g., click:run)")
	session := fs.String("session", "", "session id (defaults to TEXELUI_SESSION)")
	value := fs.String("value", "", "single widget id to return value for")
	values := fs.String("values", "", "comma-separated widget ids to return values for")
	format := fs.String("format", "event", "output: event|json|sh")
	_ = fs.Parse(args)

	req := texeluicli.Request{
		Cmd:     "wait",
		Session: resolveSession(*session),
		Events:  splitCSV(*events),
	}
	if *value != "" {
		req.Values = []string{*value}
	} else if *values != "" {
		req.Values = splitCSV(*values)
	}

	resp, err := texeluicli.SendRequest(req, socketPath)
	if err != nil {
		exitError(err)
	}
	if !resp.OK {
		exitError(fmt.Errorf(resp.Error))
	}

	if *value != "" {
		fmt.Println(resp.Values[*value])
		return
	}
	if *values != "" {
		switch strings.ToLower(*format) {
		case "sh":
			fmt.Print(formatShell(resp.Values))
		default:
			writeJSON(resp.Values)
		}
		return
	}
	fmt.Println(resp.Event)
}

func getCmd(args []string, socketPath string) {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	ids := fs.String("ids", "", "comma-separated widget ids")
	session := fs.String("session", "", "session id (defaults to TEXELUI_SESSION)")
	format := fs.String("format", "json", "output: json|sh")
	_ = fs.Parse(args)

	if *ids == "" {
		exitError(fmt.Errorf("ids required"))
	}
	req := texeluicli.Request{
		Cmd:     "get",
		Session: resolveSession(*session),
		IDs:     splitCSV(*ids),
	}
	resp, err := texeluicli.SendRequest(req, socketPath)
	if err != nil {
		exitError(err)
	}
	if !resp.OK {
		exitError(fmt.Errorf(resp.Error))
	}
	switch strings.ToLower(*format) {
	case "sh":
		fmt.Print(formatShell(resp.Values))
	default:
		writeJSON(resp.Values)
	}
}

func setCmd(args []string, socketPath string) {
	fs := flag.NewFlagSet("set", flag.ExitOnError)
	id := fs.String("id", "", "widget id")
	session := fs.String("session", "", "session id (defaults to TEXELUI_SESSION)")
	var text stringFlag
	var value stringFlag
	var checked stringFlag
	fs.Var(&text, "text", "text value")
	fs.Var(&value, "value", "value")
	fs.Var(&checked, "checked", "checkbox value (true/false)")
	_ = fs.Parse(args)

	if *id == "" {
		exitError(fmt.Errorf("id required"))
	}
	req := texeluicli.Request{Cmd: "set", Session: resolveSession(*session), ID: *id}
	if checked.set {
		v := strings.ToLower(checked.value)
		parsed := v == "true" || v == "1" || v == "yes" || v == "on"
		req.Checked = &parsed
	} else if text.set {
		req.Text = text.value
	} else if value.set {
		req.Value = value.value
	} else {
		exitError(fmt.Errorf("value required"))
	}
	resp, err := texeluicli.SendRequest(req, socketPath)
	if err != nil {
		exitError(err)
	}
	if !resp.OK {
		exitError(fmt.Errorf(resp.Error))
	}
}

func appendCmd(args []string, socketPath string) {
	fs := flag.NewFlagSet("append", flag.ExitOnError)
	id := fs.String("id", "", "widget id")
	session := fs.String("session", "", "session id (defaults to TEXELUI_SESSION)")
	text := fs.String("text", "", "text to append")
	_ = fs.Parse(args)

	if *id == "" {
		exitError(fmt.Errorf("id required"))
	}
	req := texeluicli.Request{
		Cmd:     "append",
		Session: resolveSession(*session),
		ID:      *id,
		Text:    *text,
	}
	resp, err := texeluicli.SendRequest(req, socketPath)
	if err != nil {
		exitError(err)
	}
	if !resp.OK {
		exitError(fmt.Errorf(resp.Error))
	}
}

func runCmd(args []string, socketPath string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	session := fs.String("session", "", "session id (defaults to TEXELUI_SESSION)")
	stdout := fs.String("stdout", "", "widget id for stdout")
	stderr := fs.String("stderr", "", "widget id for stderr")
	clear := fs.String("clear", "", "widget id to clear before run")
	cwd := fs.String("cwd", "", "working directory")
	_ = fs.Parse(args)
	argv := fs.Args()
	if len(argv) == 0 {
		exitError(fmt.Errorf("command required"))
	}
	req := texeluicli.Request{
		Cmd:     "run",
		Session: resolveSession(*session),
		Run: &texeluicli.RunRequest{
			Argv:   argv,
			Stdout: *stdout,
			Stderr: *stderr,
			Clear:  *clear,
			Cwd:    *cwd,
		},
	}
	resp, err := texeluicli.SendRequest(req, socketPath)
	if err != nil {
		exitError(err)
	}
	if !resp.OK {
		exitError(fmt.Errorf(resp.Error))
	}
	if resp.ExitCode != nil && *resp.ExitCode != 0 {
		os.Exit(*resp.ExitCode)
	}
}

func closeCmd(args []string, socketPath string) {
	fs := flag.NewFlagSet("close", flag.ExitOnError)
	session := fs.String("session", "", "session id (defaults to TEXELUI_SESSION)")
	_ = fs.Parse(args)

	req := texeluicli.Request{Cmd: "close", Session: resolveSession(*session)}
	resp, err := texeluicli.SendRequest(req, socketPath)
	if err != nil {
		exitError(err)
	}
	if !resp.OK {
		exitError(fmt.Errorf(resp.Error))
	}
}

func resolveSession(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if env := os.Getenv("TEXELUI_SESSION"); env != "" {
		return env
	}
	return ""
}

func splitCSV(val string) []string {
	if val == "" {
		return nil
	}
	parts := strings.Split(val, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func formatShell(values map[string]string) string {
	var b strings.Builder
	for key, val := range values {
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(shellEscape(val))
		b.WriteString("\n")
	}
	return b.String()
}

func shellEscape(val string) string {
	if val == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(val, "'", `'"'"'`) + "'"
}

type stringFlag struct {
	set   bool
	value string
}

func (s *stringFlag) String() string {
	return s.value
}

func (s *stringFlag) Set(val string) error {
	s.value = val
	s.set = true
	return nil
}

func writeJSON(values map[string]string) {
	data, err := json.Marshal(values)
	if err != nil {
		exitError(err)
	}
	fmt.Println(string(data))
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: texelui [--server] [--socket path] <command> [args]")
	fmt.Fprintln(os.Stderr, "commands: open, wait, get, set, append, run, close")
}

func exitError(err error) {
	fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(1)
}

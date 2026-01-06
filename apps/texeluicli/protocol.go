package texeluicli

type Request struct {
	Cmd     string     `json:"cmd"`
	Session string     `json:"session,omitempty"`
	Spec    *Spec      `json:"spec,omitempty"`
	Events  []string   `json:"events,omitempty"`
	IDs     []string   `json:"ids,omitempty"`
	Values  []string   `json:"values,omitempty"`
	ID      string     `json:"id,omitempty"`
	Text    string     `json:"text,omitempty"`
	Value   string     `json:"value,omitempty"`
	Checked *bool      `json:"checked,omitempty"`
	Run     *RunRequest `json:"run,omitempty"`
}

type RunRequest struct {
	Argv   []string `json:"argv,omitempty"`
	Cmd    string   `json:"cmd,omitempty"`
	Stdout string   `json:"stdout,omitempty"`
	Stderr string   `json:"stderr,omitempty"`
	Clear  string   `json:"clear,omitempty"`
	Cwd    string   `json:"cwd,omitempty"`
}

type Response struct {
	OK       bool              `json:"ok"`
	Error    string            `json:"error,omitempty"`
	Session  string            `json:"session,omitempty"`
	Event    string            `json:"event,omitempty"`
	Values   map[string]string `json:"values,omitempty"`
	ExitCode *int              `json:"exit_code,omitempty"`
}

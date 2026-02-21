package transformer

import (
	"sync/atomic"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/config"
)

// stubTransformer records calls for testing.
type stubTransformer struct {
	id            string
	handleCalls   int64
	promptCalls   int64
	commandCalls  int64
	lastLineIdx   int64
	lastIsCommand bool
	lastCommand   string
}

func (s *stubTransformer) HandleLine(lineIdx int64, line *parser.LogicalLine, isCommand bool) {
	atomic.AddInt64(&s.handleCalls, 1)
	s.lastLineIdx = lineIdx
	s.lastIsCommand = isCommand
}

func (s *stubTransformer) NotifyPromptStart() {
	atomic.AddInt64(&s.promptCalls, 1)
}

func (s *stubTransformer) NotifyCommandStart(cmd string) {
	atomic.AddInt64(&s.commandCalls, 1)
	s.lastCommand = cmd
}

func TestRegisterAndLookup(t *testing.T) {
	// Use a clean registry for this test by saving/restoring global state.
	// Since we can't easily reset the global registry, we test with unique IDs.
	const id = "test-register-lookup"

	Register(id, func(cfg Config) (Transformer, error) {
		return &stubTransformer{id: id}, nil
	})

	f, ok := Lookup(id)
	if !ok {
		t.Fatal("Lookup failed for registered transformer")
	}
	if f == nil {
		t.Fatal("Lookup returned nil factory")
	}

	tr, err := f(nil)
	if err != nil {
		t.Fatalf("factory returned error: %v", err)
	}
	if st, ok := tr.(*stubTransformer); !ok || st.id != id {
		t.Fatalf("unexpected transformer: %v", tr)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	const id = "test-duplicate-panic"

	Register(id, func(cfg Config) (Transformer, error) {
		return &stubTransformer{}, nil
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()

	Register(id, func(cfg Config) (Transformer, error) {
		return &stubTransformer{}, nil
	})
}

func TestLookupMissing(t *testing.T) {
	_, ok := Lookup("nonexistent-transformer-xyz")
	if ok {
		t.Fatal("Lookup should return false for unregistered ID")
	}
}

func TestPipelineHandleLine(t *testing.T) {
	s1 := &stubTransformer{id: "a"}
	s2 := &stubTransformer{id: "b"}
	p := &Pipeline{transformers: []Transformer{s1, s2}, enabled: true}

	line := &parser.LogicalLine{}
	suppressed := p.HandleLine(42, line, true)

	if suppressed {
		t.Error("expected no suppression from plain stub transformers")
	}
	if s1.handleCalls != 1 || s2.handleCalls != 1 {
		t.Errorf("expected 1 call each, got s1=%d s2=%d", s1.handleCalls, s2.handleCalls)
	}
	if s1.lastLineIdx != 42 || s2.lastLineIdx != 42 {
		t.Errorf("expected lineIdx=42, got s1=%d s2=%d", s1.lastLineIdx, s2.lastLineIdx)
	}
	if !s1.lastIsCommand || !s2.lastIsCommand {
		t.Error("expected isCommand=true for both")
	}
}

func TestPipelineNotifyPromptStart(t *testing.T) {
	s1 := &stubTransformer{id: "a"}
	s2 := &stubTransformer{id: "b"}
	p := &Pipeline{transformers: []Transformer{s1, s2}, enabled: true}

	p.NotifyPromptStart()

	if s1.promptCalls != 1 || s2.promptCalls != 1 {
		t.Errorf("expected 1 prompt call each, got s1=%d s2=%d", s1.promptCalls, s2.promptCalls)
	}
}

func TestBuildPipelineDisabled(t *testing.T) {
	cfg := config.Config{
		"transformers": map[string]interface{}{
			"enabled":  false,
			"pipeline": []interface{}{},
		},
	}
	p := BuildPipeline(cfg)
	if p != nil {
		t.Fatal("expected nil pipeline when disabled")
	}
}

func TestBuildPipelineMissingSection(t *testing.T) {
	cfg := config.Config{}
	p := BuildPipeline(cfg)
	if p != nil {
		t.Fatal("expected nil pipeline when section missing")
	}
}

func TestBuildPipelineSkipsDisabledEntry(t *testing.T) {
	const id = "test-build-disabled-entry"
	Register(id, func(cfg Config) (Transformer, error) {
		return &stubTransformer{id: id}, nil
	})

	cfg := config.Config{
		"transformers": map[string]interface{}{
			"enabled": true,
			"pipeline": []interface{}{
				map[string]interface{}{
					"id":      id,
					"enabled": false,
				},
			},
		},
	}
	p := BuildPipeline(cfg)
	if p != nil {
		t.Fatal("expected nil pipeline when all entries disabled")
	}
}

func TestBuildPipelineSkipsUnknown(t *testing.T) {
	cfg := config.Config{
		"transformers": map[string]interface{}{
			"enabled": true,
			"pipeline": []interface{}{
				map[string]interface{}{
					"id":      "unknown-transformer-xyz",
					"enabled": true,
				},
			},
		},
	}
	p := BuildPipeline(cfg)
	if p != nil {
		t.Fatal("expected nil pipeline when all entries unknown")
	}
}

func TestBuildPipelineSuccess(t *testing.T) {
	const id = "test-build-success"
	Register(id, func(cfg Config) (Transformer, error) {
		return &stubTransformer{id: id}, nil
	})

	cfg := config.Config{
		"transformers": map[string]interface{}{
			"enabled": true,
			"pipeline": []interface{}{
				map[string]interface{}{
					"id":      id,
					"enabled": true,
				},
			},
		},
	}
	p := BuildPipeline(cfg)
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}
	if len(p.transformers) != 1 {
		t.Fatalf("expected 1 transformer, got %d", len(p.transformers))
	}
}

func TestBuildPipelinePassesConfig(t *testing.T) {
	const id = "test-build-config-pass"
	var receivedCfg Config
	Register(id, func(cfg Config) (Transformer, error) {
		receivedCfg = cfg
		return &stubTransformer{id: id}, nil
	})

	cfg := config.Config{
		"transformers": map[string]interface{}{
			"enabled": true,
			"pipeline": []interface{}{
				map[string]interface{}{
					"id":      id,
					"enabled": true,
					"style":   "bold",
				},
			},
		},
	}
	p := BuildPipeline(cfg)
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}
	if receivedCfg == nil {
		t.Fatal("factory did not receive config")
	}
	if receivedCfg["style"] != "bold" {
		t.Errorf("expected style=bold, got %v", receivedCfg["style"])
	}
	// "id" and "enabled" should not be passed
	if _, ok := receivedCfg["id"]; ok {
		t.Error("config should not contain 'id' key")
	}
	if _, ok := receivedCfg["enabled"]; ok {
		t.Error("config should not contain 'enabled' key")
	}
}

// suppressingTransformer suppresses even-numbered lines.
type suppressingTransformer struct {
	stubTransformer
}

func (s *suppressingTransformer) ShouldSuppress(lineIdx int64) bool {
	return lineIdx%2 == 0
}

func TestPipelineSuppression(t *testing.T) {
	sup := &suppressingTransformer{}
	after := &stubTransformer{id: "after"}
	p := &Pipeline{transformers: []Transformer{sup, after}, enabled: true}

	line := &parser.LogicalLine{}

	suppressed := p.HandleLine(0, line, true)
	if !suppressed {
		t.Error("expected suppressed for even lineIdx")
	}
	if after.handleCalls != 0 {
		t.Error("expected 'after' to not be called for suppressed line")
	}

	suppressed = p.HandleLine(1, line, true)
	if suppressed {
		t.Error("expected not suppressed for odd lineIdx")
	}
	if after.handleCalls != 1 {
		t.Errorf("expected 'after' called once, got %d", after.handleCalls)
	}
}

func TestPipelineNotifyCommandStart(t *testing.T) {
	s1 := &stubTransformer{id: "a"}
	s2 := &stubTransformer{id: "b"}
	p := &Pipeline{transformers: []Transformer{s1, s2}, enabled: true}

	p.NotifyCommandStart("cat foo.go")

	if s1.commandCalls != 1 || s2.commandCalls != 1 {
		t.Errorf("expected 1 command call each, got s1=%d s2=%d", s1.commandCalls, s2.commandCalls)
	}
	if s1.lastCommand != "cat foo.go" || s2.lastCommand != "cat foo.go" {
		t.Errorf("expected lastCommand='cat foo.go', got s1=%q s2=%q", s1.lastCommand, s2.lastCommand)
	}
}

func TestPipelineEnabled(t *testing.T) {
	s := &stubTransformer{id: "a"}
	p := &Pipeline{transformers: []Transformer{s}, enabled: true}

	if !p.Enabled() {
		t.Error("expected pipeline to be enabled by default")
	}

	p.SetEnabled(false)
	if p.Enabled() {
		t.Error("expected pipeline to be disabled after SetEnabled(false)")
	}

	p.SetEnabled(true)
	if !p.Enabled() {
		t.Error("expected pipeline to be re-enabled after SetEnabled(true)")
	}
}

func TestPipelineDisabledSkipsHandleLine(t *testing.T) {
	s := &stubTransformer{id: "a"}
	p := &Pipeline{transformers: []Transformer{s}, enabled: true}

	line := &parser.LogicalLine{}

	// Enabled: should dispatch.
	p.HandleLine(1, line, false)
	if s.handleCalls != 1 {
		t.Fatalf("expected 1 handleCall when enabled, got %d", s.handleCalls)
	}

	// Disable: should skip and return false.
	p.SetEnabled(false)
	suppressed := p.HandleLine(2, line, true)
	if suppressed {
		t.Error("expected false from HandleLine when disabled")
	}
	if s.handleCalls != 1 {
		t.Errorf("expected handleCalls still 1 when disabled, got %d", s.handleCalls)
	}
}

func TestPipelineDisabledSkipsNotify(t *testing.T) {
	s := &stubTransformer{id: "a"}
	p := &Pipeline{transformers: []Transformer{s}, enabled: true}

	// Enabled: calls go through.
	p.NotifyPromptStart()
	p.NotifyCommandStart("ls")
	if s.promptCalls != 1 {
		t.Fatalf("expected 1 promptCall when enabled, got %d", s.promptCalls)
	}
	if s.commandCalls != 1 {
		t.Fatalf("expected 1 commandCall when enabled, got %d", s.commandCalls)
	}

	// Disable: calls are skipped.
	p.SetEnabled(false)
	p.NotifyPromptStart()
	p.NotifyCommandStart("pwd")
	if s.promptCalls != 1 {
		t.Errorf("expected promptCalls still 1 when disabled, got %d", s.promptCalls)
	}
	if s.commandCalls != 1 {
		t.Errorf("expected commandCalls still 1 when disabled, got %d", s.commandCalls)
	}
}

func TestBuildPipelineDefaultEnabled(t *testing.T) {
	// When "enabled" is omitted from a pipeline entry, it should default to true
	const id = "test-build-default-enabled"
	Register(id, func(cfg Config) (Transformer, error) {
		return &stubTransformer{id: id}, nil
	})

	cfg := config.Config{
		"transformers": map[string]interface{}{
			"enabled": true,
			"pipeline": []interface{}{
				map[string]interface{}{
					"id": id,
					// no "enabled" key
				},
			},
		},
	}
	p := BuildPipeline(cfg)
	if p == nil {
		t.Fatal("expected non-nil pipeline when entry has no explicit enabled flag")
	}
}

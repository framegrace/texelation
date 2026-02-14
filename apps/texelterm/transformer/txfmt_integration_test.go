package transformer_test

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/transformer"
	"github.com/framegrace/texelation/config"

	// Import txfmt for its init() side-effect registration.
	_ "github.com/framegrace/texelation/apps/texelterm/txfmt"
)

func TestTxfmtRegistered(t *testing.T) {
	_, ok := transformer.Lookup("txfmt")
	if !ok {
		t.Fatal("txfmt should be registered via init()")
	}
}

func TestTxfmtDisabledInPipeline(t *testing.T) {
	cfg := config.Config{
		"transformers": map[string]interface{}{
			"enabled": true,
			"pipeline": []interface{}{
				map[string]interface{}{
					"id":      "txfmt",
					"enabled": false,
				},
			},
		},
	}
	p := transformer.BuildPipeline(cfg)
	if p != nil {
		t.Fatal("expected nil pipeline when txfmt is disabled")
	}
}

func TestTxfmtEnabledInPipeline(t *testing.T) {
	cfg := config.Config{
		"transformers": map[string]interface{}{
			"enabled": true,
			"pipeline": []interface{}{
				map[string]interface{}{
					"id":      "txfmt",
					"enabled": true,
				},
			},
		},
	}
	p := transformer.BuildPipeline(cfg)
	if p == nil {
		t.Fatal("expected non-nil pipeline when txfmt is enabled")
	}
}

func TestTransformersGlobalDisable(t *testing.T) {
	cfg := config.Config{
		"transformers": map[string]interface{}{
			"enabled": false,
			"pipeline": []interface{}{
				map[string]interface{}{
					"id":      "txfmt",
					"enabled": true,
				},
			},
		},
	}
	p := transformer.BuildPipeline(cfg)
	if p != nil {
		t.Fatal("expected nil pipeline when transformers globally disabled")
	}
}

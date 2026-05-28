package packer

import (
	"testing"

	"context-steward/internal/config"
)

func TestHeuristicTokenizer(t *testing.T) {
	tokenizer := NewHeuristicTokenizer(4.0)
	text := "12345678"
	tokens := tokenizer.CountTokens(text)
	if tokens != 2 {
		t.Errorf("CountTokens(%q) = %d; expected 2", text, tokens)
	}
}

func TestResolveAuthority(t *testing.T) {
	cfg := &config.Config{}
	cfg.Authority.Defaults.High = []string{"README.md", "decisions/*.md"}
	cfg.Authority.Defaults.Medium = []string{"docs/*.md"}
	cfg.Authority.Defaults.Low = []string{"notes/*.md"}
	cfg.Authority.Defaults.Archival = []string{"archive/**"}

	tests := []struct {
		path     string
		expected string
	}{
		{"README.md", "high"},
		{"decisions/0001-test.md", "high"},
		{"docs/architecture.md", "medium"},
		{"notes/brainstorm.md", "low"},
		{"archive/old-design.md", "archival"},
		{"src/main.go", "low"}, // Fallback for code files since they are not md/txt
	}

	for _, tc := range tests {
		// Passing nil as db connection because we don't have database overrides in this test
		result := ResolveAuthority(nil, cfg, tc.path)
		if result != tc.expected {
			t.Errorf("ResolveAuthority(%q) = %q; expected %q", tc.path, result, tc.expected)
		}
	}
}

package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config representation of .context-steward/config.yaml
type Config struct {
	Workspace WorkspaceConfig `mapstructure:"workspace"`
	LLM       LLMConfig       `mapstructure:"llm"`
	Packets   PacketsConfig   `mapstructure:"packets"`
	Authority AuthorityConfig `mapstructure:"authority"`
}

type WorkspaceConfig struct {
	Root   string   `mapstructure:"root"`
	Ignore []string `mapstructure:"ignore"`
}

type LLMConfig struct {
	Provider string `mapstructure:"provider"`
	Endpoint string `mapstructure:"endpoint"`
	Model    string `mapstructure:"model"`
	Enabled  bool   `mapstructure:"enabled"`
}

type PacketsConfig struct {
	DefaultBudget         int    `mapstructure:"default_budget"`
	IncludeSources        bool   `mapstructure:"include_sources"`
	IncludeHealthWarnings bool   `mapstructure:"include_health_warnings"`
	Format                string `mapstructure:"format"`
}

type AuthorityConfig struct {
	Defaults struct {
		High     []string `mapstructure:"high"`
		Medium   []string `mapstructure:"medium"`
		Low      []string `mapstructure:"low"`
		Archival []string `mapstructure:"archival"`
	} `mapstructure:"defaults"`
}

// DefaultConfigYAML returns the string representation of default configuration
func DefaultConfigYAML() string {
	return `workspace:
  root: .
  ignore:
    - .git/
    - node_modules/
    - dist/
    - build/
    - vendor/
    - .context-steward/index.sqlite
llm:
  provider: ollama
  endpoint: http://localhost:11434
  model: llama3.2:3b
  enabled: true
packets:
  default_budget: 2000
  include_sources: true
  include_health_warnings: true
  format: markdown
authority:
  defaults:
    high:
      - README.md
      - docs/vision.md
      - docs/architecture.md
      - decisions/*.md
    medium:
      - planning/*.md
      - docs/*.md
    low:
      - notes/*.md
      - brainstorms/*.md
    archival:
      - archive/**
`
}

// LoadConfig loads configuration from path or defaults to .context-steward/config.yaml
func LoadConfig(cfgPath string, workspaceDir string) (*Config, error) {
	v := viper.New()

	var absWorkspace string
	var err error
	if workspaceDir != "" {
		absWorkspace, err = filepath.Abs(workspaceDir)
	} else {
		absWorkspace, err = os.Getwd()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute workspace path: %w", err)
	}

	if cfgPath != "" {
		v.SetConfigFile(cfgPath)
	} else {
		v.AddConfigPath(filepath.Join(absWorkspace, ".context-steward"))
		v.SetConfigName("config")
		v.SetConfigType("yaml")
	}

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Resolve config-specified root path relative to the workspace dir
	if cfg.Workspace.Root == "" || cfg.Workspace.Root == "." {
		cfg.Workspace.Root = absWorkspace
	} else if !filepath.IsAbs(cfg.Workspace.Root) {
		cfg.Workspace.Root = filepath.Clean(filepath.Join(absWorkspace, cfg.Workspace.Root))
	}

	return &cfg, nil
}

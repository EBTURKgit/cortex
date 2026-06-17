// Package config manages cortex project and user configuration.
// Config is loaded from cortex.yaml (project) and ~/.cortex/config.yaml (user).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EBTURKgit/cortex/internal/logging"

	"gopkg.in/yaml.v3"
)

// LLMConfig defines a single LLM provider configuration.
type LLMConfig struct {
	Provider string `yaml:"provider"` // ollama, openai, anthropic, etc.
	Model    string `yaml:"model"`    // model name
	APIKey   string `yaml:"api_key"`  // API key (or use env var)
	Endpoint string `yaml:"endpoint"` // custom endpoint URL
}

// AgentConfig defines configuration for a specific agent role.
type AgentConfig struct {
	Enabled bool     `yaml:"enabled"`
	Model   string   `yaml:"model"`  // LLM model name for this agent
	Prompt  string   `yaml:"prompt"` // custom system prompt override
	Tools   []string `yaml:"tools"`  // allowed tools
}

// ServerConfig defines the graph server settings.
type ServerConfig struct {
	Host    string `yaml:"host"`    // listen address
	Port    int    `yaml:"port"`    // listen port
	Storage string `yaml:"storage"` // memory, bolt, neo4j
	DBPath  string `yaml:"db_path"` // path for persistent storage
}

// ProjectConfig defines the project being worked on.
type ProjectConfig struct {
	Name     string   `yaml:"name"`
	RootPath string   `yaml:"root_path"`
	Language string   `yaml:"language"`
	Ignore   []string `yaml:"ignore"`
}

// Config is the full cortex configuration.
type Config struct {
	Project ProjectConfig          `yaml:"project"`
	Server  ServerConfig           `yaml:"server"`
	Agents  map[string]AgentConfig `yaml:"agents"`
	LLM     map[string]LLMConfig   `yaml:"llm"`
}

// UserConfig stores user-level settings (API keys, preferences).
type UserConfig struct {
	DefaultLLM string               `yaml:"default_llm"`
	LLM        map[string]LLMConfig `yaml:"llm"`
	Editor     string               `yaml:"editor"`
}

// DefaultConfig returns the default project configuration.
func DefaultConfig() *Config {
	logging.Debug("Creating default config")
	return &Config{
		Project: ProjectConfig{
			Name:     "my-project",
			RootPath: ".",
			Language: "auto",
			Ignore:   []string{".git", "node_modules", "vendor", ".cortex"},
		},
		Server: ServerConfig{
			Host:    "127.0.0.1",
			Port:    8741,
			Storage: "memory",
			DBPath:  ".cortex/data",
		},
		Agents: map[string]AgentConfig{
			"manager":   {Enabled: false, Model: "default"},
			"backend":   {Enabled: false, Model: "default"},
			"frontend":  {Enabled: false, Model: "default"},
			"database":  {Enabled: false, Model: "default"},
			"qa":        {Enabled: false, Model: "default"},
			"architect": {Enabled: false, Model: "default"},
		},
		LLM: map[string]LLMConfig{
			"default": {
				Provider: "ollama",
				Model:    "codellama:7b",
				Endpoint: "http://localhost:11434",
			},
		},
	}
}

// Validate checks the config for common errors.
func (c *Config) Validate() []string {
	var errs []string
	if c.Project.Name == "" {
		errs = append(errs, "project.name is required")
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		errs = append(errs, "server.port must be between 1 and 65535")
	}
	if c.Server.Storage != "memory" && c.Server.Storage != "bolt" && c.Server.Storage != "neo4j" {
		errs = append(errs, "server.storage must be 'memory', 'bolt', or 'neo4j'")
	}
	for name, llm := range c.LLM {
		if llm.Provider != "ollama" && llm.Provider != "openai" && llm.Provider != "anthropic" {
			errs = append(errs, fmt.Sprintf("llm.%s.provider unsupported: %s", name, llm.Provider))
		}
	}
	return errs
}

// LoadEnvFile loads a .env file if it exists (simple parser, no external deps).
func LoadEnvFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			val = strings.Trim(val, "\"'")
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
	logging.Debug("Loaded .env file", map[string]interface{}{"path": path})
	return nil
}

// LoadConfig reads a YAML config file and returns the parsed Config.
func LoadConfig(path string) (*Config, error) {
	logging.Debug("Loading config", map[string]interface{}{"path": path})

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logging.Debug("Config not found, using defaults", map[string]interface{}{"path": path})
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	// Apply environment overrides for API keys
	for name, llm := range cfg.LLM {
		envKey := fmt.Sprintf("CORTEX_LLM_%s_API_KEY", strings.ToUpper(name))
		if key := os.Getenv(envKey); key != "" {
			llm.APIKey = key
			cfg.LLM[name] = llm
			logging.Debug("Loaded LLM key from env", map[string]interface{}{"llm": name})
		}
	}

	// Validate config
	if errs := cfg.Validate(); len(errs) > 0 {
		for _, e := range errs {
			logging.Warn("Config issue", map[string]interface{}{"warning": e})
		}
	}

	logging.Info("Config loaded", map[string]interface{}{
		"project": cfg.Project.Name,
		"server":  fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		"storage": cfg.Server.Storage,
	})
	return cfg, nil
}

// SaveConfig writes a Config to a YAML file.
func SaveConfig(path string, cfg *Config) error {
	logging.Debug("Saving config", map[string]interface{}{"path": path})

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}

	logging.Info("Config saved", map[string]interface{}{"path": path})
	return nil
}

// LoadUserConfig reads the user-level config from ~/.cortex/config.yaml.
func LoadUserConfig() (*UserConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}

	path := filepath.Join(home, ".cortex", "config.yaml")
	logging.Debug("Loading user config", map[string]interface{}{"path": path})

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logging.Debug("User config not found, using defaults")
			return &UserConfig{
				DefaultLLM: "default",
				LLM:        make(map[string]LLMConfig),
			}, nil
		}
		return nil, fmt.Errorf("read user config: %w", err)
	}

	uc := &UserConfig{}
	if err := yaml.Unmarshal(data, uc); err != nil {
		return nil, fmt.Errorf("parse user config: %w", err)
	}

	logging.Info("User config loaded")
	return uc, nil
}

// SaveUserConfig writes the user-level config.
func SaveUserConfig(cfg *UserConfig) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}

	dir := filepath.Join(home, ".cortex")
	path := filepath.Join(dir, "config.yaml")

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	logging.Info("User config saved", map[string]interface{}{"path": path})
	return nil
}

// FindProjectConfig walks up from dir to find a cortex.yaml.
func FindProjectConfig(dir string) (string, error) {
	logging.Debug("Searching for cortex.yaml", map[string]interface{}{"from": dir})

	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	for {
		path := filepath.Join(abs, "cortex.yaml")
		if _, err := os.Stat(path); err == nil {
			logging.Debug("Found cortex.yaml", map[string]interface{}{"path": path})
			return path, nil
		}

		parent := filepath.Dir(abs)
		if parent == abs {
			// Reached root without finding
			return "", fmt.Errorf("cortex.yaml not found in any parent of %s", dir)
		}
		abs = parent
	}
}

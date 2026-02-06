package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the CLI configuration
type Config struct {
	DefaultProfile string             `yaml:"default-profile"`
	DefaultOutput  string             `yaml:"default-output,omitempty"`
	Profiles       map[string]Profile `yaml:"profiles"`
}

// Profile represents a Jamf Pro server profile
type Profile struct {
	URL          string `yaml:"url"`
	AuthMethod   string `yaml:"auth-method"` // token, basic, oauth2
	Token        string `yaml:"token,omitempty"`
	Username     string `yaml:"username,omitempty"`
	ClientID     string `yaml:"client-id,omitempty"`
	ClientSecret string `yaml:"client-secret,omitempty"`
}

// ConfigPath returns the path to the config file, preferring XDG
func ConfigPath() string {
	// XDG-compliant path
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, "jamfpro-cli", "config.yaml")
	}

	home, _ := os.UserHomeDir()

	// Check XDG default
	xdgPath := filepath.Join(home, ".config", "jamfpro-cli", "config.yaml")
	if _, err := os.Stat(xdgPath); err == nil {
		return xdgPath
	}

	// Fallback to legacy path
	legacyPath := filepath.Join(home, ".jamfpro-cli", "config.yaml")
	if _, err := os.Stat(legacyPath); err == nil {
		return legacyPath
	}

	// Default to XDG for new configs
	return xdgPath
}

// Load reads the config from disk. If the file doesn't exist, returns an
// empty config (not an error).
func Load() (*Config, error) {
	cfg := &Config{
		Profiles: make(map[string]Profile),
	}

	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Ensure the Profiles map is initialized even if YAML had none
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}

	return cfg, nil
}

// Save writes the config to disk with 0600 permissions, creating parent
// directories as needed.
func Save(cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}

	path := ConfigPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config directory %s: %w", dir, err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

// GetProfile returns the named profile, or the default profile if name is
// empty. Returns the profile and its name, or an error if not found.
func GetProfile(cfg *Config, name string) (*Profile, string, error) {
	if name == "" {
		name = cfg.DefaultProfile
	}

	if name == "" {
		return nil, "", fmt.Errorf("no profile specified and no default profile configured")
	}

	p, ok := cfg.Profiles[name]
	if !ok {
		return nil, name, fmt.Errorf("profile %q not found in config", name)
	}

	return &p, name, nil
}

// ResolveSecret resolves a secret value that may contain special prefixes:
//   - "env:VAR_NAME"   — reads the value from environment variable VAR_NAME
//   - "file:/path"     — reads the value from the file at /path
//   - anything else    — returned as-is (literal value)
func ResolveSecret(value string) (string, error) {
	if after, ok := strings.CutPrefix(value, "env:"); ok {
		envVal := os.Getenv(after)
		if envVal == "" {
			return "", fmt.Errorf("environment variable %q is not set or empty", after)
		}
		return envVal, nil
	}

	if after, ok := strings.CutPrefix(value, "file:"); ok {
		data, err := os.ReadFile(after)
		if err != nil {
			return "", fmt.Errorf("reading secret file %s: %w", after, err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	return value, nil
}

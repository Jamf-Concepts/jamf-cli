// Copyright 2026, Jamf Software LLC

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Jamf-Concepts/jamf-cli/internal/keychain"
)

// Config represents the CLI configuration
type Config struct {
	DefaultProfile string             `yaml:"default-profile"`
	DefaultOutput  string             `yaml:"default-output,omitempty"`
	Profiles       map[string]Profile `yaml:"profiles"`
	// ReportDir is the directory generated HTML reports are written to. It is
	// load-bearing for the MCP server, which refuses to write a report unless
	// an operator has designated a directory here — the connecting model never
	// supplies a path. The CLI treats it as a default destination only.
	ReportDir string `yaml:"report-dir,omitempty"`
	// UpdateCheck gates the once-a-day "a newer jamf-cli is available"
	// advisory. nil means enabled; `update-check: false` silences it for
	// every invocation, which is how an admin turns it off across a fleet
	// that upgrades through a deployed package rather than by hand.
	UpdateCheck *bool `yaml:"update-check,omitempty"`
}

// Profile represents a server profile for a Jamf product.
type Profile struct {
	Product      string `yaml:"product,omitempty"` // "pro" (default), "protect", "school", or "security"
	URL          string `yaml:"url"`
	AuthMethod   string `yaml:"auth-method"` // token, oauth2, platform, apikey, security
	Token        string `yaml:"token,omitempty"`
	ClientID     string `yaml:"client-id,omitempty"`
	ClientSecret string `yaml:"client-secret,omitempty"`
	TenantID     string `yaml:"tenant-id,omitempty"`    // platform auth
	PlatformURL  string `yaml:"platform-url,omitempty"` // school: separate gateway URL for Platform API
	NetworkID    string `yaml:"network-id,omitempty"`   // school only
	APIKey       string `yaml:"api-key,omitempty"`      // school only
	// Security (Jamf Security Cloud) only: each of the Risk, Device Lifecycle,
	// and Shared Signals & Events APIs is provisioned as its own "Security
	// Integration" with its own application ID/secret, so — unlike every other
	// product's single ClientID/ClientSecret pair — Security needs up to
	// three independent pairs. Any subset may be configured; commands for an
	// unconfigured API fail with a "run security setup" hint.
	SSEURL                string         `yaml:"sse-url,omitempty"` // separate host for Shared Signals & Events
	RiskClientID          string         `yaml:"risk-client-id,omitempty"`
	RiskClientSecret      string         `yaml:"risk-client-secret,omitempty"`
	LifecycleClientID     string         `yaml:"lifecycle-client-id,omitempty"`
	LifecycleClientSecret string         `yaml:"lifecycle-client-secret,omitempty"`
	SSEClientID           string         `yaml:"sse-client-id,omitempty"`
	SSEClientSecret       string         `yaml:"sse-client-secret,omitempty"`
	DestructiveCooldown   *time.Duration `yaml:"destructive-cooldown,omitempty"`
}

const (
	configDirName       = "jamf-cli"
	legacyConfigDirName = "jamfpro-cli"
)

// ConfigPath returns the path to the config file using XDG conventions.
func ConfigPath() string {
	return filepath.Join(configDir(), "config.yaml")
}

func configDir() string {
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, configDirName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", configDirName)
}

// ReportDirPath returns ReportDir with a leading ~ expanded, or "" when no
// report directory is configured.
func (c *Config) ReportDirPath() string {
	if c == nil || c.ReportDir == "" {
		return ""
	}
	dir := c.ReportDir
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, strings.TrimPrefix(dir[1:], "/"))
		}
	}
	return dir
}

func legacyConfigPath() string {
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, legacyConfigDirName, "config.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", legacyConfigDirName, "config.yaml")
}

// Load reads the config from disk. If the file doesn't exist, returns an
// empty config (not an error). Automatically migrates from the legacy
// ~/.config/jamfpro-cli/ location if found.
func Load() (*Config, error) {
	cfg := &Config{
		Profiles: make(map[string]Profile),
	}

	path := ConfigPath()

	// One-time migration from old config location
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if oldPath := legacyConfigPath(); oldPath != "" {
			if oldData, readErr := os.ReadFile(oldPath); readErr == nil {
				dir := filepath.Dir(path)
				if mkErr := os.MkdirAll(dir, 0o700); mkErr == nil {
					if writeErr := os.WriteFile(path, oldData, 0o600); writeErr == nil {
						fmt.Fprintf(os.Stderr, "Migrated config from %s to %s\n",
							filepath.Dir(oldPath), dir)
					}
				}
			}
		}
	}

	data, err := os.ReadFile(path)
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
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config directory %s: %w", dir, err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
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
		return nil, "", fmt.Errorf("no profile specified and no default profile configured. Run: jamf-cli config add-profile <name> --url <url>")
	}

	p, ok := cfg.Profiles[name]
	if !ok {
		return nil, name, fmt.Errorf("profile %q not found in config", name)
	}

	return &p, name, nil
}

// KeychainStore allows overriding the keychain implementation for testing.
// When nil, the real system keychain is used.
var KeychainStore keychain.Store

// GetKeychainStore returns the configured keychain store, falling back to the
// real system keychain when no override has been set via KeychainStore.
func GetKeychainStore() keychain.Store {
	if KeychainStore != nil {
		return KeychainStore
	}
	return keychain.New()
}

// ResolveSecret resolves a secret value that must use a recognized prefix:
//   - "env:VAR_NAME"        — reads the value from environment variable VAR_NAME
//   - "file:/path"          — reads the value from the file at /path
//   - "keychain:ref"        — reads the value from the system keychain
//
// Empty strings are returned as-is. Any other value is rejected.
func ResolveSecret(value string) (string, error) {
	if value == "" {
		return "", nil
	}
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

	if after, ok := strings.CutPrefix(value, "keychain:"); ok {
		store := GetKeychainStore()
		service, account := keychain.ParseRef(after)
		secret, err := store.Get(service, account)
		if err != nil {
			return "", fmt.Errorf("reading keychain item %s/%s: %w", service, account, err)
		}
		return secret, nil
	}

	return "", fmt.Errorf("unrecognized secret format %q: must use env:, file:, or keychain: prefix", value)
}

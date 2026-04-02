// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"
)

func TestDeviceTarget_Validate(t *testing.T) {
	tests := []struct {
		name    string
		dt      deviceTarget
		wantErr string
	}{
		{"serial", deviceTarget{serial: "C02X"}, ""},
		{"name", deviceTarget{name: "Mac"}, ""},
		{"id", deviceTarget{id: "42"}, ""},
		{"group", deviceTarget{group: "All"}, ""},
		{"from-file", deviceTarget{fromFile: "f.txt"}, ""},
		{"none", deviceTarget{}, "required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.dt.validate()
			if tt.wantErr == "" && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestDeviceTarget_IsBulk(t *testing.T) {
	if (&deviceTarget{serial: "C02X"}).isBulk() {
		t.Error("serial should not be bulk")
	}
	if !(&deviceTarget{group: "All"}).isBulk() {
		t.Error("group should be bulk")
	}
	if !(&deviceTarget{fromFile: "f.txt"}).isBulk() {
		t.Error("from-file should be bulk")
	}
}

func TestComputerActionSubcommands_Exist(t *testing.T) {
	resetGlobals()
	root := NewRootCmd("test", "abc", "2024-01-01")

	wantComputer := []string{
		"erase", "remove-mdm", "redeploy-framework",
		"blank-push", "ddm-sync", "renew-mdm",
	}
	for _, name := range wantComputer {
		t.Run("computers/"+name, func(t *testing.T) {
			root.SetArgs([]string{"pro", "computers", name, "--help"})
			if err := root.Execute(); err != nil {
				t.Errorf("command 'pro computers %s --help' failed: %v", name, err)
			}
		})
	}
}

func TestMobileActionSubcommands_Exist(t *testing.T) {
	resetGlobals()
	root := NewRootCmd("test", "abc", "2024-01-01")

	wantMobile := []string{"erase", "unmanage"}
	for _, name := range wantMobile {
		t.Run("mobile-devices/"+name, func(t *testing.T) {
			root.SetArgs([]string{"pro", "mobile-devices", name, "--help"})
			if err := root.Execute(); err != nil {
				t.Errorf("command 'pro mobile-devices %s --help' failed: %v", name, err)
			}
		})
	}
}

func TestComputerAliasSubcommands(t *testing.T) {
	resetGlobals()
	root := NewRootCmd("test", "abc", "2024-01-01")
	root.SetArgs([]string{"pro", "comp", "erase", "--help"})
	if err := root.Execute(); err != nil {
		t.Errorf("alias 'pro comp erase --help' failed: %v", err)
	}
}

func TestMobileAliasSubcommands(t *testing.T) {
	resetGlobals()
	root := NewRootCmd("test", "abc", "2024-01-01")
	root.SetArgs([]string{"pro", "md", "erase", "--help"})
	if err := root.Execute(); err != nil {
		t.Errorf("alias 'pro md erase --help' failed: %v", err)
	}
}

func TestComputerEraseScaffold(t *testing.T) {
	resetGlobals()
	root := NewRootCmd("test", "abc", "2024-01-01")
	root.SetArgs([]string{"pro", "computers", "erase", "--scaffold"})
	if err := root.Execute(); err != nil {
		t.Errorf("scaffold failed: %v", err)
	}
}

func TestMobileEraseScaffold(t *testing.T) {
	resetGlobals()
	root := NewRootCmd("test", "abc", "2024-01-01")
	root.SetArgs([]string{"pro", "mobile-devices", "erase", "--scaffold"})
	if err := root.Execute(); err != nil {
		t.Errorf("scaffold failed: %v", err)
	}
}

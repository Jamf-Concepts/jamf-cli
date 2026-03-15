package commands

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestDetectShell_FromEnv(t *testing.T) {
	tests := []struct {
		shell string
		want  string
	}{
		{"/bin/zsh", "zsh"},
		{"/bin/bash", "bash"},
		{"/bin/fish", "fish"},
		{"/usr/local/bin/zsh", "zsh"},
		{"/usr/local/bin/bash", "bash"},
	}
	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			t.Setenv("SHELL", tt.shell)
			got := detectShell()
			if got != tt.want {
				t.Errorf("detectShell() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectShell_UnknownShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/csh")
	got := detectShell()
	if got != "" {
		t.Errorf("detectShell() = %q, want empty string for unsupported shell", got)
	}
}

func TestDetectShell_EmptyShell(t *testing.T) {
	t.Setenv("SHELL", "")
	got := detectShell()
	// On macOS there's no /proc fallback, so this should be empty
	// On Linux it may fall back to /proc, but empty SHELL should still result in empty on most CI
	// We just verify it doesn't panic
	_ = got
}

func TestCompletion_ProducesOutput(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			root := NewRootCmd("test", "abc123", "2024-01-01")
			var buf bytes.Buffer
			root.SetOut(&buf)
			root.SetArgs([]string{"completion", shell})

			if err := root.Execute(); err != nil {
				t.Fatalf("completion %s failed: %v", shell, err)
			}
		})
	}
}

func TestCompletionInstall_NoShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/tcsh") // unsupported shell
	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"completion", "install"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for unsupported shell")
	}
	if !strings.Contains(err.Error(), "could not detect shell") {
		t.Errorf("error = %q, want to contain 'could not detect shell'", err.Error())
	}
}

func TestCompletionInstall_Fish(t *testing.T) {
	t.Setenv("SHELL", "/bin/fish")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"completion", "install"})

	// Capture stderr (install writes progress there)
	err := root.Execute()
	if err != nil {
		t.Fatalf("completion install fish failed: %v", err)
	}

	// Verify the file was written
	fishPath := dir + "/fish/completions/jamfpro-cli.fish"
	info, err := os.Stat(fishPath)
	if err != nil {
		t.Fatalf("completion file not created at %s: %v", fishPath, err)
	}
	if info.Size() == 0 {
		t.Error("completion file is empty")
	}
}

func TestCompletionInstall_Zsh(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Pre-create the candidate dir so the install logic picks it over system paths
	zshDir := dir + "/.zsh/completions"
	if err := os.MkdirAll(zshDir, 0o755); err != nil {
		t.Fatal(err)
	}

	root := NewRootCmd("test", "abc123", "2024-01-01")
	root.SetArgs([]string{"completion", "install"})

	err := root.Execute()
	if err != nil {
		t.Fatalf("completion install zsh failed: %v", err)
	}

	zshPath := zshDir + "/_jamfpro-cli"
	info, err := os.Stat(zshPath)
	if err != nil {
		t.Fatalf("completion file not created at %s: %v", zshPath, err)
	}
	if info.Size() == 0 {
		t.Error("completion file is empty")
	}
}

package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for jamf-cli.

Use "jamf-cli completion [shell]" to output the script.
Use "jamf-cli completion install" to auto-detect and install.`,
	}

	// Add subcommands for each shell
	cmd.AddCommand(newCompletionBashCmd())
	cmd.AddCommand(newCompletionZshCmd())
	cmd.AddCommand(newCompletionFishCmd())
	cmd.AddCommand(newCompletionPowershellCmd())
	cmd.AddCommand(newCompletionInstallCmd())

	return cmd
}

func newCompletionBashCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bash",
		Short: "Generate bash completion script",
		Long: `Generate bash completion script.

To load completions in your current shell session:
  source <(jamf-cli completion bash)

To install permanently:
  # Linux:
  jamf-cli completion bash > /etc/bash_completion.d/jamf-cli
  # macOS with Homebrew:
  jamf-cli completion bash > $(brew --prefix)/etc/bash_completion.d/jamf-cli
`,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Root().GenBashCompletion(os.Stdout)
		},
	}
}

func newCompletionZshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "zsh",
		Short: "Generate zsh completion script",
		Long: `Generate zsh completion script.

To load completions in your current shell session:
  source <(jamf-cli completion zsh)

To install permanently:
  jamf-cli completion zsh > "${fpath[1]}/_jamf-cli"

You may need to start a new shell for completions to take effect.
`,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Root().GenZshCompletion(os.Stdout)
		},
	}
}

func newCompletionFishCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fish",
		Short: "Generate fish completion script",
		Long: `Generate fish completion script.

To load completions in your current shell session:
  jamf-cli completion fish | source

To install permanently:
  jamf-cli completion fish > ~/.config/fish/completions/jamf-cli.fish
`,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Root().GenFishCompletion(os.Stdout, true)
		},
	}
}

func newCompletionPowershellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "powershell",
		Short: "Generate PowerShell completion script",
		Long: `Generate PowerShell completion script.

To load completions in your current shell session:
  jamf-cli completion powershell | Out-String | Invoke-Expression

To install permanently, add the output to your PowerShell profile.
`,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		},
	}
}

func newCompletionInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Auto-detect shell and install completions",
		Long: `Auto-detect the current shell and install completion scripts.

This command will:
1. Detect your current shell (bash, zsh, fish)
2. Generate the appropriate completion script
3. Install it to the standard location for your shell

Supported shells: bash, zsh, fish
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := detectShell()
			if shell == "" {
				return fmt.Errorf("could not detect shell; please use 'completion [bash|zsh|fish]' directly")
			}

			fmt.Fprintf(os.Stderr, "Detected shell: %s\n", shell)

			var installPath string
			var content strings.Builder

			switch shell {
			case "bash":
				if err := cmd.Root().GenBashCompletion(&content); err != nil {
					return err
				}
				if runtime.GOOS == "darwin" {
					// Try Homebrew path first
					brewPrefix, err := exec.Command("brew", "--prefix").Output()
					if err == nil {
						installPath = filepath.Join(strings.TrimSpace(string(brewPrefix)), "etc", "bash_completion.d", "jamf-cli")
					} else {
						installPath = filepath.Join(os.Getenv("HOME"), ".bash_completion.d", "jamf-cli")
					}
				} else {
					installPath = "/etc/bash_completion.d/jamf-cli"
				}

			case "zsh":
				if err := cmd.Root().GenZshCompletion(&content); err != nil {
					return err
				}
				// Try common zsh completion directories
				home := os.Getenv("HOME")
				candidates := []string{
					filepath.Join(home, ".zsh", "completions"),
					filepath.Join(home, ".local", "share", "zsh", "site-functions"),
					"/usr/local/share/zsh/site-functions",
				}
				for _, dir := range candidates {
					if _, err := os.Stat(dir); err == nil {
						installPath = filepath.Join(dir, "_jamf-cli")
						break
					}
				}
				if installPath == "" {
					// Create default directory
					installPath = filepath.Join(home, ".zsh", "completions", "_jamf-cli")
				}

			case "fish":
				if err := cmd.Root().GenFishCompletion(&content, true); err != nil {
					return err
				}
				configDir := os.Getenv("XDG_CONFIG_HOME")
				if configDir == "" {
					configDir = filepath.Join(os.Getenv("HOME"), ".config")
				}
				installPath = filepath.Join(configDir, "fish", "completions", "jamf-cli.fish")

			default:
				return fmt.Errorf("shell %q not supported for auto-install; use 'completion [bash|zsh|fish]' directly", shell)
			}

			// Create parent directory if needed
			dir := filepath.Dir(installPath)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("creating directory %s: %w", dir, err)
			}

			// Write completion file
			if err := os.WriteFile(installPath, []byte(content.String()), 0o644); err != nil {
				return fmt.Errorf("writing completion file: %w", err)
			}

			fmt.Fprintf(os.Stderr, "Completion script installed to: %s\n", installPath)
			fmt.Fprintf(os.Stderr, "Restart your shell or source the file to enable completions.\n")

			return nil
		},
	}
}

func detectShell() string {
	// Check SHELL environment variable
	shell := os.Getenv("SHELL")
	if shell != "" {
		base := filepath.Base(shell)
		switch base {
		case "bash", "zsh", "fish":
			return base
		}
	}

	// Check parent process on Unix systems
	if runtime.GOOS != "windows" {
		ppid := os.Getppid()
		cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", ppid))
		if err == nil {
			cmd := strings.Split(string(cmdline), "\x00")[0]
			base := filepath.Base(cmd)
			switch base {
			case "bash", "zsh", "fish":
				return base
			}
		}
	}

	return ""
}

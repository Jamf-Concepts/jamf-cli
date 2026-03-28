package commands

import (
	"fmt"
	"io"
	"os"
)

// readProtectInput reads JSON input from --from-file flag or stdin.
func readProtectInput(fromFile string) ([]byte, error) {
	if fromFile != "" {
		data, err := os.ReadFile(fromFile)
		if err != nil {
			return nil, fmt.Errorf("reading input file: %w", err)
		}
		return data, nil
	}

	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 10<<20))
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
		if len(data) > 0 {
			return data, nil
		}
	}

	return nil, fmt.Errorf("input required: use --from-file or pipe JSON to stdin")
}

// confirmProtectDelete prompts for confirmation before a destructive operation.
// Returns true if the operation should proceed, false if it was dry-run/aborted.
func confirmProtectDelete(resourceType, name string, yes bool) (bool, error) {
	if dryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] Would delete %s %q\n", resourceType, name)
		return false, nil
	}
	if !yes {
		if noInput {
			return false, fmt.Errorf("destructive operation requires --yes when --no-input is set")
		}
		fmt.Fprintf(os.Stderr, "This will delete %s %q. Type 'yes' to confirm: ", resourceType, name)
		var confirm string
		fmt.Scanln(&confirm)
		if confirm != "yes" {
			return false, fmt.Errorf("aborted")
		}
	}
	return true, nil
}

// confirmProtectReplace prompts for confirmation before replacing an existing resource.
// Returns true if the operation should proceed, false if it was dry-run/aborted.
func confirmProtectReplace(resourceType, name string, yes bool) (bool, error) {
	if dryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] Would replace %s %q\n", resourceType, name)
		return false, nil
	}
	if !yes {
		if noInput {
			return false, fmt.Errorf("destructive operation requires --yes when --no-input is set")
		}
		fmt.Fprintf(os.Stderr, "%s %q already exists and will be replaced. Type 'yes' to confirm: ", resourceType, name)
		var confirm string
		fmt.Scanln(&confirm)
		if confirm != "yes" {
			return false, fmt.Errorf("aborted")
		}
	}
	return true, nil
}

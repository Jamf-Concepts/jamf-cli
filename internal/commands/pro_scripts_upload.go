// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newScriptsUploadCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		filePath   string
		scriptName string
		categoryID string
		priority   string
		flagYes    bool
	)

	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload a script file to Jamf Pro",
		Long: `Upload a local script file to Jamf Pro.

If a script with the same name already exists, it is updated with the new
file contents. Otherwise a new script record is created.`,
		Example: `  # Upload a script
  jamf-cli pro scripts upload --file ./deploy-chrome.sh --name "Deploy Chrome"

  # Upload using the filename as the script name
  jamf-cli pro scripts upload --file ./deploy-chrome.sh

  # Upload with category and priority
  jamf-cli pro scripts upload --file ./deploy-chrome.sh --name "Deploy Chrome" --category-id 5 --priority BEFORE`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			client := cliCtx.Client

			// Read the script file
			content, err := os.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("reading script file: %w", err)
			}

			if scriptName == "" {
				scriptName = filepath.Base(filePath)
			}

			fmt.Fprintf(os.Stderr, "Script: %s (%d bytes)\n", scriptName, len(content))

			// Check for existing script by name
			existingID, err := resolveScriptByName(ctx, client, scriptName)
			if err != nil {
				return err
			}

			if existingID != "" {
				if !flagYes {
					noInput, _ := cmd.Flags().GetBool("no-input")
					if noInput {
						return fmt.Errorf("script %q already exists (id %s); use --yes to replace when --no-input is set", scriptName, existingID)
					}
					fmt.Fprintf(os.Stderr, "Script %q already exists (id %s). Replace? Type 'yes' to confirm: ", scriptName, existingID)
					var confirm string
					_, _ = fmt.Scanln(&confirm)
					if confirm != "yes" {
						return fmt.Errorf("aborted")
					}
				}

				// Update existing script
				fmt.Fprintf(os.Stderr, "Updating existing script (id %s)...\n", existingID)
				if err := updateScript(ctx, client, existingID, scriptName, string(content), categoryID, priority); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Updated successfully\n")

				data, err := fetchJSON(ctx, client, "/v1/scripts/"+existingID)
				if err != nil {
					return nil
				}
				result, _ := json.Marshal(data)
				return cliCtx.Output.PrintRaw(result)
			}

			// Create new script
			fmt.Fprintf(os.Stderr, "Creating script %q...\n", scriptName)
			newID, err := createScript(ctx, client, scriptName, string(content), categoryID, priority)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Created successfully (id %s)\n", newID)

			data, err := fetchJSON(ctx, client, "/v1/scripts/"+newID)
			if err != nil {
				return nil
			}
			result, _ := json.Marshal(data)
			return cliCtx.Output.PrintRaw(result)
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "path to the script file (required)")
	_ = cmd.MarkFlagRequired("file")
	cmd.Flags().StringVar(&scriptName, "name", "", "script name in Jamf Pro (defaults to filename)")
	cmd.Flags().StringVar(&categoryID, "category-id", "-1", "category ID")
	cmd.Flags().StringVar(&priority, "priority", "AFTER", "execution priority: BEFORE, AFTER, AT_REBOOT")
	cmd.Flags().BoolVar(&flagYes, "yes", false, "skip confirmation when replacing an existing script")

	return cmd
}

func resolveScriptByName(ctx context.Context, client registry.HTTPClient, name string) (string, error) {
	data, err := fetchJSON(ctx, client, fmt.Sprintf("/v1/scripts?filter=%s&page-size=1",
		url.QueryEscape(fmt.Sprintf(`name=="%s"`, name))))
	if err != nil {
		return "", fmt.Errorf("searching for script: %w", err)
	}

	totalCount, _ := data["totalCount"].(float64)
	if totalCount == 0 {
		return "", nil
	}

	results, ok := data["results"].([]any)
	if !ok || len(results) == 0 {
		return "", nil
	}
	first, ok := results[0].(map[string]any)
	if !ok {
		return "", nil
	}
	return extractField(first, "id"), nil
}

func createScript(ctx context.Context, client registry.HTTPClient, name, contents, categoryID, priority string) (string, error) {
	payload := map[string]any{
		"name":           name,
		"scriptContents": contents,
		"categoryId":     categoryID,
		"priority":       priority,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(ctx, "POST", "/v1/scripts", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parsing create response: %w", err)
	}
	id := extractField(result, "id")
	if id == "" {
		// Extract ID from href: "/api/v1/scripts/42"
		if href, ok := result["href"].(string); ok {
			parts := strings.Split(href, "/")
			id = parts[len(parts)-1]
		}
	}
	if id == "" {
		return "", fmt.Errorf("no id in create response: %s", string(respBody))
	}
	return id, nil
}

func updateScript(ctx context.Context, client registry.HTTPClient, id, name, contents, categoryID, priority string) error {
	// Fetch current to preserve fields
	data, err := fetchJSON(ctx, client, "/v1/scripts/"+id)
	if err != nil {
		return fmt.Errorf("fetching script for update: %w", err)
	}

	data["name"] = name
	data["scriptContents"] = contents
	if categoryID != "" {
		data["categoryId"] = categoryID
	}
	if priority != "" {
		data["priority"] = priority
	}

	body, err := json.Marshal(data)
	if err != nil {
		return err
	}

	resp, err := client.Do(ctx, "PUT", "/v1/scripts/"+id, bytes.NewReader(body))
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

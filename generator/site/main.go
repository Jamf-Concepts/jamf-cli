// Copyright 2026, Jamf Software LLC

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type siteData struct {
	GeneratedAt  time.Time     `json:"generatedAt"`
	Version      string        `json:"version"`
	CommandCount int           `json:"commandCount"`
	NewCommands  []string      `json:"newCommands,omitempty"`
	Commands     []siteCommand `json:"commands"`
}

type siteCommand struct {
	Command     string   `json:"command"`
	Description string   `json:"description"`
	Aliases     []string `json:"aliases"`
	Flags       []string `json:"flags"`
	Product     string   `json:"product,omitempty"`
	Group       string   `json:"group,omitempty"`
}

type rawCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
	Aliases     string `json:"aliases"`
	Flags       string `json:"flags"`
	Product     string `json:"product"`
	Group       string `json:"group"`
}

func main() {
	binary := flag.String("binary", "./bin/jamf-cli", "path to jamf-cli binary")
	output := flag.String("output", "docs/site/commands.json", "output file path")
	previous := flag.String("previous", "", "path to previous commands.json for new-command detection")
	flag.Parse()

	versionOut, err := exec.Command(*binary, "version").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error running %s version: %v\n", *binary, err)
		os.Exit(1)
	}
	version := parseVersion(string(versionOut))

	commandsOut, err := exec.Command(*binary, "commands", "-o", "json").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error running %s commands: %v\n", *binary, err)
		os.Exit(1)
	}

	var previousCommands map[string]bool
	var prevNewCommands []string
	if *previous != "" {
		previousCommands, prevNewCommands, err = loadPreviousCommands(*previous)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not load previous commands: %v\n", err)
		}
	}

	result, err := transformCommands(commandsOut, version, previousCommands, prevNewCommands)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error transforming commands: %v\n", err)
		os.Exit(1)
	}

	if *output == "/dev/stdout" || *output == "-" {
		_, err = os.Stdout.Write(result)
	} else {
		err = os.WriteFile(*output, result, 0o644)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error writing output: %v\n", err)
		os.Exit(1)
	}
}

func transformCommands(rawJSON []byte, version string, previousCommands map[string]bool, prevNewCommands []string) ([]byte, error) {
	var raw []rawCommand
	if err := json.Unmarshal(rawJSON, &raw); err != nil {
		return nil, fmt.Errorf("parsing commands JSON: %w", err)
	}

	currentCommands := make(map[string]bool, len(raw))
	commands := make([]siteCommand, len(raw))
	var newCommands []string
	for i, r := range raw {
		commands[i] = siteCommand{
			Command:     r.Command,
			Description: r.Description,
			Aliases:     splitCSV(r.Aliases),
			Flags:       splitCSV(r.Flags),
			Product:     r.Product,
			Group:       r.Group,
		}
		currentCommands[r.Command] = true
		if previousCommands != nil && !previousCommands[r.Command] {
			newCommands = append(newCommands, r.Command)
		}
	}

	// If no new commands were detected in this diff, carry forward the
	// previous deploy's new-command list (filtered to commands that still
	// exist). This keeps "New" badges visible across non-release deploys.
	if len(newCommands) == 0 && len(prevNewCommands) > 0 {
		for _, cmd := range prevNewCommands {
			if currentCommands[cmd] {
				newCommands = append(newCommands, cmd)
			}
		}
	}

	data := siteData{
		GeneratedAt:  time.Now().UTC(),
		Version:      version,
		CommandCount: len(commands),
		NewCommands:  newCommands,
		Commands:     commands,
	}

	return json.MarshalIndent(data, "", "  ")
}

func loadPreviousCommands(path string) (map[string]bool, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var prev siteData
	if err := json.Unmarshal(data, &prev); err != nil {
		return nil, nil, err
	}
	m := make(map[string]bool, len(prev.Commands))
	for _, c := range prev.Commands {
		m[c.Command] = true
	}
	return m, prev.NewCommands, nil
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func parseVersion(output string) string {
	line, _, _ := strings.Cut(output, "\n")
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "unknown"
	}
	v := fields[1]
	// Strip git-describe suffix (e.g. "v1.2.0-52-gffc0b5a-dirty" → "v1.2.0")
	if parts := strings.SplitN(v, "-", 2); len(parts) == 2 && strings.ContainsAny(parts[1], "0123456789") {
		if _, err := strconv.Atoi(strings.Split(parts[1], "-")[0]); err == nil {
			v = parts[0]
		}
	}
	// Normalize: strip "v" prefix so display layer can format consistently
	v = strings.TrimPrefix(v, "v")
	return v
}

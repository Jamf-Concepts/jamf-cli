// Copyright 2026, Jamf Software LLC

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type siteData struct {
	GeneratedAt  time.Time     `json:"generatedAt"`
	Version      string        `json:"version"`
	CommandCount int           `json:"commandCount"`
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

	result, err := transformCommands(commandsOut, version)
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

func transformCommands(rawJSON []byte, version string) ([]byte, error) {
	var raw []rawCommand
	if err := json.Unmarshal(rawJSON, &raw); err != nil {
		return nil, fmt.Errorf("parsing commands JSON: %w", err)
	}

	commands := make([]siteCommand, len(raw))
	for i, r := range raw {
		commands[i] = siteCommand{
			Command:     r.Command,
			Description: r.Description,
			Aliases:     splitCSV(r.Aliases),
			Flags:       splitCSV(r.Flags),
			Product:     r.Product,
			Group:       r.Group,
		}
	}

	data := siteData{
		GeneratedAt:  time.Now().UTC(),
		Version:      version,
		CommandCount: len(commands),
		Commands:     commands,
	}

	return json.MarshalIndent(data, "", "  ")
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
	return fields[1]
}

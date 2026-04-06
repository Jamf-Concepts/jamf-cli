// Copyright 2026, Jamf Software LLC

package main

import (
	"slices"
	"testing"
)

func TestInjectEnvArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  string
		want []string
	}{
		{
			name: "empty env leaves args unchanged",
			args: []string{"jamf-cli", "pro", "computers", "list"},
			env:  "",
			want: []string{"jamf-cli", "pro", "computers", "list"},
		},
		{
			name: "single flag is prepended after program name",
			args: []string{"jamf-cli", "pro", "computers", "list"},
			env:  "--quiet",
			want: []string{"jamf-cli", "--quiet", "pro", "computers", "list"},
		},
		{
			name: "multiple flags are prepended in order",
			args: []string{"jamf-cli", "pro", "computers", "list"},
			env:  "--quiet --no-input --no-color",
			want: []string{"jamf-cli", "--quiet", "--no-input", "--no-color", "pro", "computers", "list"},
		},
		{
			name: "extra whitespace in env is ignored",
			args: []string{"jamf-cli", "pro", "computers", "list"},
			env:  "  --quiet   --no-input  ",
			want: []string{"jamf-cli", "--quiet", "--no-input", "pro", "computers", "list"},
		},
		{
			name: "user args are not clobbered",
			args: []string{"jamf-cli", "--output", "json", "pro", "computers", "list"},
			env:  "--quiet",
			want: []string{"jamf-cli", "--quiet", "--output", "json", "pro", "computers", "list"},
		},
		{
			name: "env flags injected with no trailing user args",
			args: []string{"jamf-cli"},
			env:  "--quiet",
			want: []string{"jamf-cli", "--quiet"},
		},
		{
			name: "whitespace-only env leaves args unchanged",
			args: []string{"jamf-cli", "pro", "computers", "list"},
			env:  "   ",
			want: []string{"jamf-cli", "pro", "computers", "list"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := injectEnvArgs(tt.args, tt.env)
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// Copyright 2026, Jamf Software LLC

package main

import (
	"runtime/debug"
	"slices"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	buildInfo := func(v string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			info := &debug.BuildInfo{}
			info.Main.Version = v
			return info, true
		}
	}
	noBuildInfo := func() (*debug.BuildInfo, bool) { return nil, false }

	tests := []struct {
		name           string
		ldflagsVersion string
		readBuildInfo  func() (*debug.BuildInfo, bool)
		want           string
	}{
		{
			name:           "ldflags version wins",
			ldflagsVersion: "v1.25.2",
			readBuildInfo:  buildInfo("v1.0.0"),
			want:           "v1.25.2",
		},
		{
			name:           "go install falls back to the module version",
			ldflagsVersion: "dev",
			readBuildInfo:  buildInfo("v1.26.0"),
			want:           "v1.26.0",
		},
		{
			name:           "local go build stays dev",
			ldflagsVersion: "dev",
			readBuildInfo:  buildInfo("(devel)"),
			want:           "dev",
		},
		{
			name:           "empty module version stays dev",
			ldflagsVersion: "dev",
			readBuildInfo:  buildInfo(""),
			want:           "dev",
		},
		{
			name:           "missing build info stays dev",
			ldflagsVersion: "dev",
			readBuildInfo:  noBuildInfo,
			want:           "dev",
		},
		{
			name:           "empty ldflags version also falls back",
			ldflagsVersion: "",
			readBuildInfo:  buildInfo("v1.26.0"),
			want:           "v1.26.0",
		},
		{
			name:           "git describe version is left alone",
			ldflagsVersion: "v1.25.2-3-gabc1234-dirty",
			readBuildInfo:  buildInfo("v1.26.0"),
			want:           "v1.25.2-3-gabc1234-dirty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersion(tt.ldflagsVersion, tt.readBuildInfo); got != tt.want {
				t.Errorf("resolveVersion(%q) = %q, want %q", tt.ldflagsVersion, got, tt.want)
			}
		})
	}
}

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
		{
			name: "quoted value is treated as single token",
			args: []string{"jamf-cli", "pro", "computers", "list"},
			env:  `--profile "My CI Profile"`,
			want: []string{"jamf-cli", "--profile", "My CI Profile", "pro", "computers", "list"},
		},
		{
			name: "invalid shlex leaves args unchanged",
			args: []string{"jamf-cli", "pro", "computers", "list"},
			env:  `--profile "unterminated`,
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

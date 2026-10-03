// Copyright 2026, Jamf Software LLC

package commands

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloudDistributionPointList_RedactsTheKeyOnlyInAnMCPChild(t *testing.T) {
	const privateKey = "-----BEGIN RSA PRIVATE KEY-----MIIEcanary"
	const password = "cdn-password-canary"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cloud-distribution-point", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cdnType":"AMAZON_S3","keyPairId":"APKAEXAMPLE","privateKey":"` + privateKey + `","password":"` + password + `","expirationSeconds":3600}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	run := func(t *testing.T, mcpChild, format string) string {
		t.Helper()
		resetGlobals()
		t.Cleanup(resetGlobals)
		isolated := t.TempDir()
		t.Setenv("HOME", isolated)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(isolated, "config"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(isolated, "cache"))
		t.Setenv("JAMF_PROFILE", "")
		t.Setenv("JAMF_URL", srv.URL)
		t.Setenv("JAMF_TOKEN", "fake-token")
		t.Setenv("JAMF_CLIENT_ID", "")
		t.Setenv("JAMF_CLIENT_SECRET", "")
		t.Setenv("JAMF_CLI_ARGS", "")
		t.Setenv(mcpChildEnvVar, mcpChild)

		args := []string{"pro", "cloud-distribution-point", "list", "-o", format}
		if mcpChild == "1" {
			childArgs, err := buildChildArgs("", args)
			if err != nil {
				t.Fatalf("run_command refuses %v: %v", args, err)
			}
			args = childArgs
		}
		stdout, stderr, err := runRoot(t, args...)
		if err != nil {
			t.Fatalf("%v: %v\nstderr: %s", args, err, stderr)
		}
		return stdout
	}

	for _, format := range []string{"json", "table"} {
		t.Run("mcp/"+format, func(t *testing.T) {
			out := run(t, "1", format)
			if strings.Contains(out, privateKey) || strings.Contains(out, password) {
				t.Errorf("-o %s in an MCP child printed a CDN credential:\n%s", format, out)
			}
			if !strings.Contains(out, protectRedacted) || !strings.Contains(out, "APKAEXAMPLE") {
				t.Errorf("-o %s in an MCP child should print the record with the marker:\n%s", format, out)
			}
		})
		t.Run("cli/"+format, func(t *testing.T) {
			out := run(t, "", format)
			if !strings.Contains(out, privateKey) || strings.Contains(out, protectRedacted) {
				t.Errorf("-o %s outside MCP should print the key unchanged:\n%s", format, out)
			}
		})
	}
}

// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"strings"
	"testing"
)

func TestResolvePolicyByNameOrID_ByID(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			if method == "GET" && path == "/JSSResource/policies/id/10" {
				return 200, `<?xml version="1.0" encoding="UTF-8"?><policy><general><id>10</id><name>Deploy Chrome</name></general></policy>`, nil
			}
			t.Fatalf("unexpected request: %s %s", method, path)
			return 0, "", nil
		},
	}

	id, name, err := resolvePolicyByNameOrID(context.Background(), client, "10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "10" {
		t.Errorf("id = %q, want %q", id, "10")
	}
	if name != "Deploy Chrome" {
		t.Errorf("name = %q, want %q", name, "Deploy Chrome")
	}
}

func TestResolvePolicyByNameOrID_ByName(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			// ID lookup fails
			if method == "GET" && path == "/JSSResource/policies/id/Deploy Chrome" {
				return 404, ``, nil
			}
			// List all policies
			if method == "GET" && path == "/JSSResource/policies" {
				return 200, `<?xml version="1.0" encoding="UTF-8"?><policies><policy><id>10</id><name>Deploy Chrome</name></policy><policy><id>20</id><name>Install Firefox</name></policy></policies>`, nil
			}
			t.Fatalf("unexpected request: %s %s", method, path)
			return 0, "", nil
		},
	}

	id, name, err := resolvePolicyByNameOrID(context.Background(), client, "Deploy Chrome")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "10" {
		t.Errorf("id = %q, want %q", id, "10")
	}
	if name != "Deploy Chrome" {
		t.Errorf("name = %q, want %q", name, "Deploy Chrome")
	}
}

func TestResolvePolicyByNameOrID_ByNameCaseInsensitive(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			if method == "GET" && path == "/JSSResource/policies/id/deploy chrome" {
				return 404, ``, nil
			}
			if method == "GET" && path == "/JSSResource/policies" {
				return 200, `<?xml version="1.0" encoding="UTF-8"?><policies><policy><id>10</id><name>Deploy Chrome</name></policy></policies>`, nil
			}
			t.Fatalf("unexpected request: %s %s", method, path)
			return 0, "", nil
		},
	}

	id, name, err := resolvePolicyByNameOrID(context.Background(), client, "deploy chrome")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "10" {
		t.Errorf("id = %q, want %q", id, "10")
	}
	if name != "Deploy Chrome" {
		t.Errorf("name = %q, want %q", name, "Deploy Chrome")
	}
}

func TestResolvePolicyByNameOrID_NotFound(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			if method == "GET" && strings.HasPrefix(path, "/JSSResource/policies/id/") {
				return 404, ``, nil
			}
			if method == "GET" && path == "/JSSResource/policies" {
				return 200, `<?xml version="1.0" encoding="UTF-8"?><policies><policy><id>10</id><name>Deploy Chrome</name></policy></policies>`, nil
			}
			t.Fatalf("unexpected request: %s %s", method, path)
			return 0, "", nil
		},
	}

	_, _, err := resolvePolicyByNameOrID(context.Background(), client, "Nonexistent Policy")
	if err == nil {
		t.Fatal("expected error for policy not found, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "not found")
	}
}

func TestRunPolicyExecute_DryRun(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			// Policy resolution by ID
			if method == "GET" && path == "/JSSResource/policies/id/10" {
				return 200, `<?xml version="1.0" encoding="UTF-8"?><policy><general><id>10</id><name>Deploy Chrome</name></general></policy>`, nil
			}
			// Device resolution by ID
			if method == "GET" && path == "/v3/computers-inventory-detail/42" {
				return 200, `{"id":"42","general":{"name":"MacBook-Lab1"}}`, nil
			}
			// Any POST means we tried to execute — that's a bug in dry-run
			if method == "POST" {
				t.Fatalf("dry-run should not POST, got: %s %s", method, path)
			}
			t.Fatalf("unexpected request: %s %s", method, path)
			return 0, "", nil
		},
	}

	err := runPolicyExecute(context.Background(), client, "10", []string{"42"}, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPolicyExecute_NoTargets(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			t.Fatalf("unexpected request: %s %s — should fail before HTTP call", method, path)
			return 0, "", nil
		},
	}

	err := runPolicyExecute(context.Background(), client, "10", nil, "", true)
	if err == nil {
		t.Fatal("expected error for no targets, got nil")
	}
	if !strings.Contains(err.Error(), "--target") || !strings.Contains(err.Error(), "--group") {
		t.Errorf("error = %q, want it to mention --target and --group", err.Error())
	}
}

func TestRunPolicyExecute_MutuallyExclusive(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			t.Fatalf("unexpected request: %s %s — should fail before HTTP call", method, path)
			return 0, "", nil
		},
	}

	err := runPolicyExecute(context.Background(), client, "10", []string{"42"}, "Lab Macs", true)
	if err == nil {
		t.Fatal("expected error for mutually exclusive flags, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %q, want it to mention mutually exclusive", err.Error())
	}
}

func TestRunPolicyExecute_ExecuteSuccess(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			// Policy resolution
			if method == "GET" && path == "/JSSResource/policies/id/10" {
				return 200, `<?xml version="1.0" encoding="UTF-8"?><policy><general><id>10</id><name>Deploy Chrome</name></general></policy>`, nil
			}
			// Device resolution
			if method == "GET" && path == "/v3/computers-inventory-detail/42" {
				return 200, `{"id":"42","general":{"name":"MacBook-Lab1"}}`, nil
			}
			// Policy execution
			if method == "POST" && path == "/JSSResource/computercommands/command/PolicyExecution/action/command/id/42/policyid/10" {
				return 201, `<computer_command/>`, nil
			}
			t.Fatalf("unexpected request: %s %s", method, path)
			return 0, "", nil
		},
	}

	err := runPolicyExecute(context.Background(), client, "10", []string{"42"}, "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPolicyExecute_PartialFailure(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			// Policy resolution
			if method == "GET" && path == "/JSSResource/policies/id/10" {
				return 200, `<?xml version="1.0" encoding="UTF-8"?><policy><general><id>10</id><name>Deploy Chrome</name></general></policy>`, nil
			}
			// Device resolution for both devices
			if method == "GET" && path == "/v3/computers-inventory-detail/42" {
				return 200, `{"id":"42","general":{"name":"MacBook-Lab1"}}`, nil
			}
			if method == "GET" && path == "/v3/computers-inventory-detail/99" {
				return 200, `{"id":"99","general":{"name":"MacBook-Lab2"}}`, nil
			}
			// First device succeeds
			if method == "POST" && path == "/JSSResource/computercommands/command/PolicyExecution/action/command/id/42/policyid/10" {
				return 201, `<computer_command/>`, nil
			}
			// Second device fails
			if method == "POST" && path == "/JSSResource/computercommands/command/PolicyExecution/action/command/id/99/policyid/10" {
				return 409, `<error>conflict</error>`, nil
			}
			t.Fatalf("unexpected request: %s %s", method, path)
			return 0, "", nil
		},
	}

	err := runPolicyExecute(context.Background(), client, "10", []string{"42", "99"}, "", true)
	if err == nil {
		t.Fatal("expected error for partial failure, got nil")
	}
	if !strings.Contains(err.Error(), "1 of 2") {
		t.Errorf("error = %q, want it to contain failure count", err.Error())
	}
}

func TestPostCommand(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			if method == "POST" && path == "/JSSResource/some/path" {
				return 201, `<ok/>`, nil
			}
			t.Fatalf("unexpected request: %s %s", method, path)
			return 0, "", nil
		},
	}

	err := postCommand(context.Background(), client, "/JSSResource/some/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPostCommand_Failure(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			if method == "POST" && path == "/JSSResource/some/path" {
				return 500, `<error>internal</error>`, nil
			}
			t.Fatalf("unexpected request: %s %s", method, path)
			return 0, "", nil
		},
	}

	err := postCommand(context.Background(), client, "/JSSResource/some/path")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want it to contain status code", err.Error())
	}
}

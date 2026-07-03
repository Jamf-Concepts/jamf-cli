// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// retryTasksMockClient serves computer resolution, deployment task listing,
// and the retry POST for the retry-failed command tests.
type retryTasksMockClient struct {
	handler func(method, path string, body []byte) (int, string, error)
	posts   []capturedPost
}

type capturedPost struct {
	path string
	body []byte
}

func (m *retryTasksMockClient) Do(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
	var b []byte
	if body != nil {
		var err error
		b, err = io.ReadAll(body)
		if err != nil {
			return nil, err
		}
	}
	if method == "POST" {
		m.posts = append(m.posts, capturedPost{path: path, body: b})
	}
	code, respBody, err := m.handler(method, path, b)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader(respBody)),
		Header:     make(http.Header),
	}, nil
}

func runRetryFailed(mock *retryTasksMockClient, args ...string) (string, error) {
	cliCtx := &registry.CLIContext{Client: mock, Output: &discardOutput{}}
	cmd := newJamfProtectDeploymentRetryFailedCmd(cliCtx)
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stderr.String(), err
}

const failedTasksPage = `{"totalCount":2,"results":[
  {"id":"82","computerId":"42","computerName":"Lab Mac 1","status":"GAVE_UP"},
  {"id":"83","computerId":"99","computerName":"Lab Mac 2","status":"GAVE_UP"}
]}`

func computerBySerialResponse(id, serial, managementID string) string {
	return fmt.Sprintf(`{"totalCount":1,"results":[{"id":"%s","udid":"","general":{"name":"Lab Mac","managementId":"%s"},"hardware":{"serialNumber":"%s"}}]}`, id, managementID, serial)
}

func computerByUDIDResponse(id, udid string) string {
	return fmt.Sprintf(`{"totalCount":1,"results":[{"id":"%s","udid":"%s","general":{"name":"Lab Mac"},"hardware":{}}]}`, id, udid)
}

// --- Target-mode validation ---

func TestRetryFailed_RequiresTargetMode(t *testing.T) {
	mock := &retryTasksMockClient{handler: func(_, _ string, _ []byte) (int, string, error) {
		return 0, "", fmt.Errorf("unexpected request")
	}}
	_, err := runRetryFailed(mock, "deploy-1")
	if err == nil || !strings.Contains(err.Error(), "one of --serial, --management-id, --udid, --all-failed, or --task-ids is required") {
		t.Fatalf("err = %v, want target-mode requirement", err)
	}
}

func TestRetryFailed_MutuallyExclusiveTargetModes(t *testing.T) {
	mock := &retryTasksMockClient{handler: func(_, _ string, _ []byte) (int, string, error) {
		return 0, "", fmt.Errorf("unexpected request")
	}}
	_, err := runRetryFailed(mock, "deploy-1", "--serial", "C02X1234", "--all-failed")
	if err == nil || !strings.Contains(err.Error(), "none of the others can be") {
		t.Fatalf("err = %v, want mutually-exclusive error", err)
	}
}

func TestRetryFailed_IncludeSucceeded_RequiresSerialOrManagementID(t *testing.T) {
	mock := &retryTasksMockClient{handler: func(_, _ string, _ []byte) (int, string, error) {
		return 0, "", fmt.Errorf("unexpected request")
	}}
	_, err := runRetryFailed(mock, "deploy-1", "--all-failed", "--include-succeeded", "--yes")
	if err == nil || !strings.Contains(err.Error(), "--include-succeeded requires --serial, --management-id, or --udid") {
		t.Fatalf("err = %v, want include-succeeded requirement", err)
	}
}

// --- --task-ids: no lookup ---

func TestRetryFailed_TaskIDs_PostsDirectlyWithoutLookup(t *testing.T) {
	mock := &retryTasksMockClient{handler: func(method, path string, _ []byte) (int, string, error) {
		if method == "GET" {
			return 0, "", fmt.Errorf("unexpected GET %s — --task-ids must skip lookup", path)
		}
		return 204, "", nil
	}}
	_, err := runRetryFailed(mock, "deploy-1", "--task-ids", "82,83")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(mock.posts))
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal(mock.posts[0].body, &body); err != nil {
		t.Fatalf("unmarshalling post body: %v", err)
	}
	if len(body.IDs) != 2 || body.IDs[0] != "82" || body.IDs[1] != "83" {
		t.Errorf("ids = %v, want [82 83]", body.IDs)
	}
}

// --- --all-failed ---

func TestRetryFailed_AllFailed_NothingToRetry(t *testing.T) {
	mock := &retryTasksMockClient{handler: func(method, path string, _ []byte) (int, string, error) {
		if strings.Contains(path, "/tasks") && method == "GET" {
			return 200, `{"totalCount":0,"results":[]}`, nil
		}
		return 0, "", fmt.Errorf("unexpected request: %s %s", method, path)
	}}
	stderr, err := runRetryFailed(mock, "deploy-1", "--all-failed", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.posts) != 0 {
		t.Errorf("expected no POST when nothing to retry, got %d", len(mock.posts))
	}
	if !strings.Contains(stderr, "Nothing to retry") {
		t.Errorf("stderr = %q, want it to mention nothing to retry", stderr)
	}
}

func TestRetryFailed_AllFailed_RequiresYes(t *testing.T) {
	mock := &retryTasksMockClient{handler: func(method, path string, _ []byte) (int, string, error) {
		if strings.Contains(path, "/tasks") && method == "GET" {
			return 200, failedTasksPage, nil
		}
		return 0, "", fmt.Errorf("unexpected request: %s %s", method, path)
	}}
	stderr, err := runRetryFailed(mock, "deploy-1", "--all-failed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.posts) != 0 {
		t.Errorf("expected no POST without --yes, got %d", len(mock.posts))
	}
	if !strings.Contains(stderr, "Use --yes to execute") {
		t.Errorf("stderr = %q, want it to prompt for --yes", stderr)
	}
}

// mixedStatusTasksPage includes a successfully-installed task (VERIFIED_INSTALL,
// the real status string observed on a live tenant) alongside failed ones, so
// tests can verify isFailedTaskStatus actually excludes non-failed tasks
// rather than trivially matching everything.
const mixedStatusTasksPage = `{"totalCount":3,"results":[
  {"id":"82","computerId":"42","computerName":"Lab Mac 1","status":"GAVE_UP"},
  {"id":"83","computerId":"99","computerName":"Lab Mac 2","status":"GAVE_UP"},
  {"id":"84","computerId":"55","computerName":"Lab Mac 3","status":"VERIFIED_INSTALL"}
]}`

func TestRetryFailed_AllFailed_WithYes_PostsAllFailedIDs(t *testing.T) {
	mock := &retryTasksMockClient{handler: func(method, path string, _ []byte) (int, string, error) {
		if strings.Contains(path, "/tasks/retry") {
			return 204, "", nil
		}
		if strings.Contains(path, "/tasks") && method == "GET" {
			if strings.Contains(path, "filter=") {
				return 0, "", fmt.Errorf("expected no server-side filter (status filtering is broken server-side; must be client-side), got: %s", path)
			}
			return 200, mixedStatusTasksPage, nil
		}
		return 0, "", fmt.Errorf("unexpected request: %s %s", method, path)
	}}
	_, err := runRetryFailed(mock, "deploy-1", "--all-failed", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(mock.posts))
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	_ = json.Unmarshal(mock.posts[0].body, &body)
	// Task 84 (VERIFIED_INSTALL) must be excluded — only the two Failed tasks retry.
	if len(body.IDs) != 2 || body.IDs[0] != "82" || body.IDs[1] != "83" {
		t.Errorf("ids = %v, want [82 83] (VERIFIED_INSTALL task 84 excluded)", body.IDs)
	}
}

// --- --serial / --management-id: resolve then client-side match ---

func TestRetryFailed_BySerial_FiltersToMatchingComputerClientSide(t *testing.T) {
	mock := &retryTasksMockClient{handler: func(method, path string, _ []byte) (int, string, error) {
		if strings.Contains(path, "/tasks/retry") {
			return 204, "", nil
		}
		if strings.Contains(path, "computers-inventory") {
			if !strings.Contains(path, "serialNumber") {
				return 0, "", fmt.Errorf("expected serial filter, got: %s", path)
			}
			return 200, computerBySerialResponse("42", "C02X1234", "mgmt-1"), nil
		}
		if strings.Contains(path, "/tasks") && method == "GET" {
			return 200, failedTasksPage, nil
		}
		return 0, "", fmt.Errorf("unexpected request: %s %s", method, path)
	}}
	_, err := runRetryFailed(mock, "deploy-1", "--serial", "C02X1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(mock.posts))
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	_ = json.Unmarshal(mock.posts[0].body, &body)
	// failedTasksPage has task 82 for computerId 42 and task 83 for computerId 99;
	// only 82 should be retried for computer 42.
	if len(body.IDs) != 1 || body.IDs[0] != "82" {
		t.Errorf("ids = %v, want [82] (client-side computerId match)", body.IDs)
	}
}

func TestRetryFailed_ByManagementID_Resolves(t *testing.T) {
	mock := &retryTasksMockClient{handler: func(method, path string, _ []byte) (int, string, error) {
		if strings.Contains(path, "/tasks/retry") {
			return 204, "", nil
		}
		if strings.Contains(path, "computers-inventory") {
			if !strings.Contains(path, "managementId") {
				return 0, "", fmt.Errorf("expected managementId filter, got: %s", path)
			}
			return 200, computerBySerialResponse("99", "C02X9999", "mgmt-2"), nil
		}
		if strings.Contains(path, "/tasks") && method == "GET" {
			return 200, failedTasksPage, nil
		}
		return 0, "", fmt.Errorf("unexpected request: %s %s", method, path)
	}}
	_, err := runRetryFailed(mock, "deploy-1", "--management-id", "mgmt-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(mock.posts))
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	_ = json.Unmarshal(mock.posts[0].body, &body)
	if len(body.IDs) != 1 || body.IDs[0] != "83" {
		t.Errorf("ids = %v, want [83] (computerId 99)", body.IDs)
	}
}

func TestRetryFailed_ByUDID_Resolves(t *testing.T) {
	mock := &retryTasksMockClient{handler: func(method, path string, _ []byte) (int, string, error) {
		if strings.Contains(path, "/tasks/retry") {
			return 204, "", nil
		}
		if strings.Contains(path, "computers-inventory") {
			if !strings.Contains(path, "udid") {
				return 0, "", fmt.Errorf("expected udid filter, got: %s", path)
			}
			return 200, computerByUDIDResponse("42", "00008030-001A2D"), nil
		}
		if strings.Contains(path, "/tasks") && method == "GET" {
			return 200, failedTasksPage, nil
		}
		return 0, "", fmt.Errorf("unexpected request: %s %s", method, path)
	}}
	_, err := runRetryFailed(mock, "deploy-1", "--udid", "00008030-001A2D")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(mock.posts))
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	_ = json.Unmarshal(mock.posts[0].body, &body)
	if len(body.IDs) != 1 || body.IDs[0] != "82" {
		t.Errorf("ids = %v, want [82] (computerId 42)", body.IDs)
	}
}

func TestRetryFailed_NoMatchingTask_ErrorsClearly(t *testing.T) {
	mock := &retryTasksMockClient{handler: func(method, path string, _ []byte) (int, string, error) {
		if strings.Contains(path, "computers-inventory") {
			return 200, computerBySerialResponse("7", "C02XNOTASK", "mgmt-7"), nil
		}
		if strings.Contains(path, "/tasks") && method == "GET" {
			return 200, failedTasksPage, nil
		}
		return 0, "", fmt.Errorf("unexpected request: %s %s", method, path)
	}}
	_, err := runRetryFailed(mock, "deploy-1", "--serial", "C02XNOTASK")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no deployment task found") {
		t.Errorf("err = %v, want it to mention no matching task", err)
	}
	if len(mock.posts) != 0 {
		t.Errorf("expected no POST when no task matches, got %d", len(mock.posts))
	}
}

// --- --dry-run ---

func TestRetryFailed_DryRun_DoesNotPost(t *testing.T) {
	resetGlobals()
	dryRun = true
	defer func() { dryRun = false }()

	mock := &retryTasksMockClient{handler: func(method, path string, _ []byte) (int, string, error) {
		if strings.Contains(path, "/tasks") && method == "GET" {
			return 200, failedTasksPage, nil
		}
		return 0, "", fmt.Errorf("unexpected request: %s %s", method, path)
	}}
	stderr, err := runRetryFailed(mock, "deploy-1", "--all-failed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.posts) != 0 {
		t.Errorf("expected no POST in dry-run, got %d", len(mock.posts))
	}
	if !strings.Contains(stderr, "[dry-run]") || !strings.Contains(stderr, "82") || !strings.Contains(stderr, "83") {
		t.Errorf("stderr = %q, want dry-run preview listing task IDs", stderr)
	}
}

// --- Retry POST error surfacing ---

func TestRetryFailed_HTTP400_SurfacesCloudServicesMessage(t *testing.T) {
	mock := &retryTasksMockClient{handler: func(method, path string, _ []byte) (int, string, error) {
		if strings.Contains(path, "/tasks/retry") {
			return 400, `{"httpStatus":400,"errors":[{"description":"Cloud Services Connection has not been established"}]}`, nil
		}
		return 0, "", fmt.Errorf("unexpected request: %s %s", method, path)
	}}
	_, err := runRetryFailed(mock, "deploy-1", "--task-ids", "82")
	if err == nil || !strings.Contains(err.Error(), "isn't registered with Jamf Protect") {
		t.Fatalf("err = %v, want the Cloud Services Connection hint", err)
	}
}

// --- isFailedTaskStatus ---

func TestIsFailedTaskStatus(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"GAVE_UP", true},
		{"gave_up", true},
		{"VERIFIED_INSTALL", false},
		{"INSTALL_IN_PROGRESS", false},
		{"COMPLETE", false},
		{"Failed", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isFailedTaskStatus(c.status); got != c.want {
			t.Errorf("isFailedTaskStatus(%q) = %v, want %v", c.status, got, c.want)
		}
	}
}

// --- fetchDeploymentTasks pagination ---

func TestFetchDeploymentTasks_PagesUntilExhausted(t *testing.T) {
	page0 := make([]string, 100)
	for i := range page0 {
		page0[i] = fmt.Sprintf(`{"id":"%d","computerId":"1","status":"GAVE_UP"}`, i)
	}
	page0Body := fmt.Sprintf(`{"totalCount":101,"results":[%s]}`, strings.Join(page0, ","))
	page1Body := `{"totalCount":101,"results":[{"id":"100","computerId":"1","status":"GAVE_UP"}]}`

	var gets []string
	mock := &retryTasksMockClient{handler: func(method, path string, _ []byte) (int, string, error) {
		gets = append(gets, path)
		if strings.Contains(path, "page=0") {
			return 200, page0Body, nil
		}
		if strings.Contains(path, "page=1") {
			return 200, page1Body, nil
		}
		return 0, "", fmt.Errorf("unexpected page request: %s", path)
	}}

	tasks, err := fetchDeploymentTasks(context.Background(), mock, "deploy-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 101 {
		t.Errorf("len(tasks) = %d, want 101", len(tasks))
	}
	if len(gets) != 2 {
		t.Errorf("GET calls = %d, want 2 (paginated)", len(gets))
	}
}

// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
)

// invalidIDBody is Jamf Pro 11.x's answer to a non-numeric id on
// /v4/computers-inventory-detail/{id}.
const invalidIDBody = `{"httpStatus":400,"errors":[{"code":"INVALID_ID","description":"id field must be string of positive numeric value or -1","id":"0","field":"id"}]}`

// wireDeviceClient answers the detail endpoint the way the server does: 400
// INVALID_ID for an id that is not numeric, 404 for an unknown numeric one.
func wireDeviceClient(search func(path string) (int, string)) (*deviceResolveMockClient, *[]string) {
	var calls []string
	return &deviceResolveMockClient{
		handler: func(_, path string) (int, string, error) {
			calls = append(calls, path)
			if id, ok := strings.CutPrefix(path, "/v4/computers-inventory-detail/"); ok {
				if !resolve.IsNumericID(id) {
					return 400, invalidIDBody, nil
				}
				return 404, `{"httpStatus":404,"errors":[]}`, nil
			}
			code, body := search(path)
			return code, body, nil
		},
	}, &calls
}

func TestResolveDeviceByIdentifier_SerialAndNameSurviveTheInvalidIDAnswer(t *testing.T) {
	client, calls := wireDeviceClient(func(path string) (int, string) {
		switch {
		case strings.Contains(path, "hardware.serialNumber") && strings.Contains(path, "FVFC41HCLYWP"):
			return 200, `{"totalCount":1,"results":[{"id":"4","general":{"name":"Lab Mac"},"hardware":{"serialNumber":"FVFC41HCLYWP"}}]}`
		case strings.Contains(path, "hardware.serialNumber"):
			return 200, `{"totalCount":0,"results":[]}`
		case strings.Contains(path, "general.name"):
			return 200, `{"totalCount":1,"results":[{"id":"31","general":{"name":"Lab Mac 2"}}]}`
		}
		return 500, ""
	})

	for _, tc := range []struct{ arg, wantID string }{
		{"FVFC41HCLYWP", "4"},
		{"Lab Mac 2", "31"},
	} {
		*calls = nil
		id, _, err := resolveDeviceByIdentifier(context.Background(), client, tc.arg)
		if err != nil {
			t.Fatalf("resolve %q: %v", tc.arg, err)
		}
		if id != tc.wantID {
			t.Errorf("resolve %q = %q, want %q", tc.arg, id, tc.wantID)
		}
		for _, c := range *calls {
			if strings.HasPrefix(c, "/v4/computers-inventory-detail/") {
				t.Errorf("resolve %q probed %s; a non-numeric identifier is not an ID", tc.arg, c)
			}
		}
	}
}

func TestTryDeviceByID_InvalidIDIsNoDevice(t *testing.T) {
	answer := func(code int, body string) *deviceResolveMockClient {
		return &deviceResolveMockClient{handler: func(_, _ string) (int, string, error) { return code, body, nil }}
	}
	ctx := context.Background()

	if _, _, err := tryDeviceByID(ctx, answer(400, invalidIDBody), "12345678901234567890"); !errors.Is(err, errNoDeviceWithID) {
		t.Errorf("400 INVALID_ID: err = %v, want errNoDeviceWithID", err)
	}
	for _, tc := range []struct {
		code int
		body string
	}{
		{400, `{"httpStatus":400,"errors":[{"code":"INVALID_FIELD"}]}`},
		{400, `not json`},
		{401, invalidIDBody},
		{403, `{"httpStatus":403,"errors":[]}`},
		{500, ``},
	} {
		_, _, err := tryDeviceByID(ctx, answer(tc.code, tc.body), "7")
		if err == nil || errors.Is(err, errNoDeviceWithID) {
			t.Errorf("HTTP %d %s: err = %v, want a failure that stops resolution", tc.code, tc.body, err)
		}
	}
}

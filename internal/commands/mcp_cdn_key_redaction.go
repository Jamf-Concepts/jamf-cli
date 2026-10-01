// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// cloudDistributionPointSecrets are the fields of the cloud distribution point
// that work outside an MCP server: the CloudFront signing key and the CDN
// password. keyPairId is the public half's ID and stays.
var cloudDistributionPointSecrets = []string{"privateKey", "password"}

// cdnKeyRedactingClient replaces the cloud distribution point's credential
// fields with <redacted> in every response to its GET, so each output format
// renders the redacted body. Installed only in an MCP child.
type cdnKeyRedactingClient struct {
	inner registry.HTTPClient
}

func (c *cdnKeyRedactingClient) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	resp, err := c.inner.Do(ctx, method, path, body)
	if err != nil {
		return resp, err
	}
	if p, _, _ := strings.Cut(path, "?"); method != "GET" || p != "/v1/cloud-distribution-point" {
		return resp, nil
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	raw = redactCloudDistributionPoint(raw)
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	resp.ContentLength = int64(len(raw))
	if resp.Header != nil {
		resp.Header.Set("Content-Length", strconv.Itoa(len(raw)))
	}
	return resp, nil
}

func redactCloudDistributionPoint(raw []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var obj map[string]any
	if dec.Decode(&obj) != nil {
		return raw
	}
	changed := false
	for _, k := range cloudDistributionPointSecrets {
		if v, ok := obj[k]; ok && v != nil && v != "" {
			obj[k] = protectRedacted
			changed = true
		}
	}
	if !changed {
		return raw
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(obj)
	return bytes.TrimSuffix(out.Bytes(), []byte("\n"))
}

package client

import (
	"testing"
)

func TestRedactTokenBody(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{
			`{"access_token":"supersecret","expires_in":900,"token_type":"Bearer"}`,
			`{"access_token":"[REDACTED]","expires_in":900,"token_type":"Bearer"}`,
		},
		{
			`{"refresh_token":"abc123","access_token":"xyz"}`,
			`{"refresh_token":"[REDACTED]","access_token":"[REDACTED]"}`,
		},
		{
			`{"data":"no tokens here"}`,
			`{"data":"no tokens here"}`,
		},
	}
	for _, c := range cases {
		got := string(RedactTokenBody([]byte(c.in)))
		if got != c.want {
			t.Errorf("RedactTokenBody(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

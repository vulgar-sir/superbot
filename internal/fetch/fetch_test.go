package fetch

import (
	"net"
	"net/url"
	"testing"
)

func TestCheckIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1",
		"10.1.2.3",
		"172.16.0.1",
		"192.168.1.1",
		"169.254.169.254",
		"100.64.0.1",
		"::1",
		"fc00::1",
	}
	for _, raw := range blocked {
		if err := checkIP(net.ParseIP(raw)); err == nil {
			t.Fatalf("expected %s to be blocked", raw)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34"}
	for _, raw := range allowed {
		if err := checkIP(net.ParseIP(raw)); err != nil {
			t.Fatalf("expected %s to be allowed: %v", raw, err)
		}
	}
}

func TestValidateURL(t *testing.T) {
	cases := []struct {
		raw     string
		wantErr bool
	}{
		{"https://example.com/a", false},
		{"http://example.com", false},
		{"ftp://example.com", true},
		{"https://user:pass@example.com", true},
		{"https:///path", true},
	}
	for _, tc := range cases {
		parsed, err := url.Parse(tc.raw)
		if err != nil {
			if !tc.wantErr {
				t.Fatalf("parse %q: %v", tc.raw, err)
			}
			continue
		}
		if err := validateURL(parsed); (err != nil) != tc.wantErr {
			t.Fatalf("validate %q error = %v, wantErr %v", tc.raw, err, tc.wantErr)
		}
	}
}

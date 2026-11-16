package dockerproxy

import (
	"net/http"
	"net/url"
	"testing"
)

func TestContainerListIsForcedToSandboxContainers(t *testing.T) {
	u, _ := url.Parse("/v1.47/containers/json?all=1&filters=%7B%22name%22%3A%7B%22postgres%22%3Atrue%7D%7D")
	rt, err := parseRoute(http.MethodGet, u)
	if err != nil || rt.kind != routeContainerList {
		t.Fatalf("list should be allowed: %v %v", rt, err)
	}
	query, err := checkQuery(rt, u.RawQuery)
	if err != nil {
		t.Fatalf("checkQuery: %v", err)
	}
	values, _ := url.ParseQuery(query)
	if got := values.Get("filters"); got != `{"label":{"superbot.bot_id":true}}` {
		t.Fatalf("the caller's filter must be replaced by the sandbox label filter, got %q", got)
	}
	rt.setUpstream("")
	if rt.upstreamPath != "/v1.47/containers/json" {
		t.Fatalf("forwarded path = %q", rt.upstreamPath)
	}
	if values.Get("all") != "1" {
		t.Fatal("all=1 must pass through")
	}
	if _, err := checkQuery(rt, "size=1"); err == nil {
		t.Fatal("other list parameters stay refused")
	}
	if _, err := parseRoute(http.MethodPost, u); err == nil {
		t.Fatal("only GET may list")
	}
}

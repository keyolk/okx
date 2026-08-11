package config

import (
	"strings"
	"testing"
)

func TestCacheKeyIsScopedByTokenWithoutExposingIt(t *testing.T) {
	first := Config{OrgURL: "https://example.okta.com", Token: "secret-one"}
	second := Config{OrgURL: first.OrgURL, Token: "secret-two"}

	if first.CacheKey() == second.CacheKey() {
		t.Fatal("CacheKey is identical for different API tokens")
	}
	if strings.Contains(first.CacheKey(), first.Token) {
		t.Fatal("CacheKey exposes the API token")
	}
	if !strings.HasPrefix(first.CacheKey(), "example.okta.com-") {
		t.Fatalf("CacheKey = %q, want org prefix", first.CacheKey())
	}
}

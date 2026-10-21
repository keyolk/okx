package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes an okta.yaml in a temp dir and clears the OKTA_* env, so
// neither the developer's real ~/.okta/okta.yaml nor their shell leaks in.
func writeConfig(t *testing.T, client string) string {
	t.Helper()
	for _, k := range []string{"OKTA_ORG_URL", "OKTA_API_TOKEN", "OKTA_ADMIN_URL"} {
		t.Setenv(k, "")
	}
	path := filepath.Join(t.TempDir(), "okta.yaml")
	body := "okta:\n  client:\n    token: test-token\n" + client
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustLoad(t *testing.T, path string) Config {
	t.Helper()
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestAdminURLFromTheFileIsNormalized(t *testing.T) {
	path := writeConfig(t, "    orgUrl: https://login.example.com\n    adminUrl: ' https://example-admin.okta.com/ '\n")
	if got := mustLoad(t, path).AdminURL; got != "https://example-admin.okta.com" {
		t.Fatalf("AdminURL = %q", got)
	}
}

func TestAdminURLEnvOverridesTheFile(t *testing.T) {
	path := writeConfig(t, "    orgUrl: https://acme.okta.com\n    adminUrl: https://file-admin.okta.com\n")
	t.Setenv("OKTA_ADMIN_URL", "https://env-admin.okta.com/")
	if got := mustLoad(t, path).AdminURL; got != "https://env-admin.okta.com" {
		t.Fatalf("AdminURL = %q, want the env value", got)
	}
}

func TestAdminURLIsDerivedOnlyForOktaHostedOrgs(t *testing.T) {
	for org, want := range map[string]string{
		"https://acme.okta.com":           "https://acme-admin.okta.com",
		"https://acme.okta.com/":          "https://acme-admin.okta.com",
		"https://Acme.OktaPreview.com":    "https://acme-admin.oktapreview.com",
		"https://acme.okta-emea.com":      "https://acme-admin.okta-emea.com",
		"https://acme-admin.okta.com":     "https://acme-admin.okta.com",
		"https://login.example.com":       "", // custom domain: never guess
		"https://okta.com":                "",
		"https://a.b.okta.com":            "",
		"https://acme.okta.com.evil.io":   "",
		"https://acme.okta.com:8443":      "",
		"http://acme.okta.com":            "",
		"https://acme.okta.com/some/path": "",
		"::not a url":                     "",
	} {
		path := writeConfig(t, "    orgUrl: '"+org+"'\n")
		if got := mustLoad(t, path).AdminURL; got != want {
			t.Errorf("orgUrl %q: AdminURL = %q, want %q", org, got, want)
		}
	}
}

func TestInvalidAdminURLIsALoadErrorNamingTheKey(t *testing.T) {
	path := writeConfig(t, "    orgUrl: https://acme.okta.com\n    adminUrl: 'javascript:alert(1)'\n")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "okta.client.adminUrl") {
		t.Fatalf("err = %v, want one naming okta.client.adminUrl", err)
	}

	t.Setenv("OKTA_ADMIN_URL", "file:///etc/passwd")
	_, err = Load(path)
	if err == nil || !strings.Contains(err.Error(), "$OKTA_ADMIN_URL") {
		t.Fatalf("err = %v, want one naming $OKTA_ADMIN_URL", err)
	}
}

func TestAdminPageURLs(t *testing.T) {
	c := Config{AdminURL: "https://acme-admin.okta.com"}
	for _, tc := range []struct{ got, want string }{
		{c.UserURL("00u1"), "https://acme-admin.okta.com/admin/user/profile/view/00u1"},
		{c.GroupURL("00g1"), "https://acme-admin.okta.com/admin/group/00g1"},
		{c.AppURL("oidc_client", "0oa1"), "https://acme-admin.okta.com/admin/app/oidc_client/instance/0oa1/"},
		// IDs and names come from the API: a slash or a ? must not change the path.
		{c.AppURL("a/b", "x?y"), "https://acme-admin.okta.com/admin/app/a%2Fb/instance/x%3Fy/"},
		{c.UserURL(""), ""},
		{c.GroupURL(""), ""},
		{c.AppURL("", "0oa1"), ""},
		{c.AppURL("oidc_client", ""), ""},
		{Config{}.UserURL("00u1"), ""},
		{Config{}.AppURL("oidc_client", "0oa1"), ""},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

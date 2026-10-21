package config

import (
	"net/url"
	"strings"
)

// oktaDomains are the Okta-hosted org domains whose admin console lives at
// <org>-admin.<domain>. A custom domain (login.example.com) has no such rule:
// its admin console is still on the Okta-hosted name, which the org URL does
// not reveal, so nothing is derived and the user sets adminUrl instead.
var oktaDomains = []string{"okta.com", "oktapreview.com", "okta-emea.com"}

// deriveAdminURL maps https://<org>.okta.com to https://<org>-admin.okta.com
// (and the same for the other Okta domains). Anything else returns "": a
// guessed admin URL that 404s is worse than no link.
func deriveAdminURL(orgURL string) string {
	u, err := url.Parse(orgURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	for _, d := range oktaDomains {
		org, ok := strings.CutSuffix(host, "."+d)
		if !ok || org == "" || strings.Contains(org, ".") {
			continue
		}
		// orgUrl may already be the admin host; do not make it -admin-admin.
		if !strings.HasSuffix(org, "-admin") {
			org += "-admin"
		}
		return "https://" + org + "." + d
	}
	return ""
}

// UserURL is the user's profile page in the admin console, or "" when the
// admin URL is unknown or the ID is empty.
func (c Config) UserURL(id string) string {
	return c.adminURL("admin/user/profile/view", id)
}

// GroupURL is the group's page in the admin console, or "".
func (c Config) GroupURL(id string) string {
	return c.adminURL("admin/group", id)
}

// AppURL is the app instance's page in the admin console, or "". The console
// addresses an instance by its app name (the integration key, e.g.
// "oidc_client"), not the label, so both are required.
func (c Config) AppURL(name, id string) string {
	if name == "" {
		return ""
	}
	u := c.adminURL("admin/app/"+url.PathEscape(name)+"/instance", id)
	if u == "" {
		return ""
	}
	// The console's own links end the instance path with a slash.
	return u + "/"
}

// adminURL joins the admin origin, a fixed path and an escaped ID. IDs come
// from the API, so they are escaped rather than trusted to be URL-safe.
func (c Config) adminURL(path, id string) string {
	if c.AdminURL == "" || id == "" {
		return ""
	}
	return c.AdminURL + "/" + path + "/" + url.PathEscape(id)
}

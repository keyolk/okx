// Package config loads Okta connection settings.
//
// The org URL and API token come from the same ~/.okta/okta.yaml that the
// official `okta` CLI uses, so there is nothing new to provision. Environment
// variables override the file for one-off runs against another org.
//
// The admin console URL (okta.client.adminUrl, $OKTA_ADMIN_URL) is okx's own
// optional key; the okta CLI ignores it. When it is unset, it is derived from
// the org URL only for Okta-hosted domains (see deriveAdminURL); a custom
// domain derives nothing and the TUI then shows no admin links.
package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/keyolk/tuikit/link"
	"gopkg.in/yaml.v3"
)

// Config holds everything needed to talk to an Okta org.
type Config struct {
	OrgURL string
	Token  string
	// AdminURL is the admin console origin, e.g. https://acme-admin.okta.com.
	// Empty means unknown: no admin links are shown.
	AdminURL string
}

// oktaYAML mirrors the subset of ~/.okta/okta.yaml that we read.
type oktaYAML struct {
	Okta struct {
		Client struct {
			OrgURL   string `yaml:"orgUrl"`
			Token    string `yaml:"token"`
			AdminURL string `yaml:"adminUrl"`
		} `yaml:"client"`
	} `yaml:"okta"`
}

// DefaultPath returns the conventional location of the Okta CLI config.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".okta/okta.yaml"
	}
	return filepath.Join(home, ".okta", "okta.yaml")
}

// Load reads config from path (empty means DefaultPath), then applies
// OKTA_ORG_URL / OKTA_API_TOKEN / OKTA_ADMIN_URL overrides. A missing file is not an error as
// long as the environment supplies both values.
func Load(path string) (Config, error) {
	if path == "" {
		path = DefaultPath()
	}

	var cfg Config
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		var y oktaYAML
		if err := yaml.Unmarshal(raw, &y); err != nil {
			return cfg, fmt.Errorf("parse %s: %w", path, err)
		}
		cfg.OrgURL = y.Okta.Client.OrgURL
		cfg.Token = y.Okta.Client.Token
		cfg.AdminURL = y.Okta.Client.AdminURL
	case !os.IsNotExist(err):
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}

	if v := os.Getenv("OKTA_ORG_URL"); v != "" {
		cfg.OrgURL = v
	}
	if v := os.Getenv("OKTA_API_TOKEN"); v != "" {
		cfg.Token = v
	}
	adminKey := "okta.client.adminUrl in " + path
	if v := os.Getenv("OKTA_ADMIN_URL"); v != "" {
		cfg.AdminURL = v
		adminKey = "$OKTA_ADMIN_URL"
	}

	cfg.OrgURL = strings.TrimRight(strings.TrimSpace(cfg.OrgURL), "/")
	cfg.Token = strings.TrimSpace(cfg.Token)
	cfg.AdminURL = strings.TrimRight(strings.TrimSpace(cfg.AdminURL), "/")

	if cfg.OrgURL == "" {
		return cfg, fmt.Errorf("no Okta org URL: set okta.client.orgUrl in %s or $OKTA_ORG_URL", path)
	}
	if cfg.Token == "" {
		return cfg, fmt.Errorf("no Okta API token: set okta.client.token in %s or $OKTA_API_TOKEN", path)
	}
	// A bad admin URL fails loudly rather than silently dropping the links:
	// the user set it on purpose, and the TUI would otherwise just look as if
	// the feature did not exist.
	if cfg.AdminURL != "" && !link.Valid(cfg.AdminURL) {
		return cfg, fmt.Errorf("%s: %q is not an http(s) URL", adminKey, cfg.AdminURL)
	}
	if cfg.AdminURL == "" {
		cfg.AdminURL = deriveAdminURL(cfg.OrgURL)
	}
	return cfg, nil
}

// CacheKey namespaces snapshots by both org and API token. Okta limits app
// visibility to the token owner's admin scope, so reusing an org-only cache
// after switching tokens can show the wrong apps. Only a short one-way digest
// is persisted in the filename; the token itself never leaves config memory.
func (c Config) CacheKey() string {
	sum := sha256.Sum256([]byte(c.Token))
	return fmt.Sprintf("%s-%x", c.OrgName(), sum[:6])
}

// OrgName extracts the org subdomain, used to namespace the cache directory so
// that switching orgs never reads another org's cached data.
func (c Config) OrgName() string {
	s := strings.TrimPrefix(strings.TrimPrefix(c.OrgURL, "https://"), "http://")
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	return strings.ReplaceAll(s, ":", "_")
}

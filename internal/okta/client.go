// Package okta is a minimal client for the parts of the Okta management API
// that app assignment work touches: apps, users, groups, and the app-user /
// app-group assignment resources.
//
// We hand-roll this instead of pulling in okta-sdk-golang because the surface
// we need is seven endpoints, and the SDK's generated model churn has been a
// recurring upgrade cost.
package okta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Client talks to one Okta org.
type Client struct {
	orgURL string
	token  string
	http   *http.Client
}

// New builds a client for the given org URL and SSWS API token.
func New(orgURL, token string) *Client {
	return &Client{
		orgURL: strings.TrimRight(orgURL, "/"),
		token:  token,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// APIError is a structured Okta error response.
type APIError struct {
	Status  int
	Code    string `json:"errorCode"`
	Summary string `json:"errorSummary"`
	ID      string `json:"errorId"`
	Causes  []struct {
		Summary string `json:"errorCauseSummary"`
	} `json:"errorCauses"`
}

func (e *APIError) Error() string {
	msg := e.Summary
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", e.Status)
	}
	for _, c := range e.Causes {
		if c.Summary != "" {
			msg += "; " + c.Summary
		}
	}
	if e.Code != "" {
		return fmt.Sprintf("okta: %s (%s)", msg, e.Code)
	}
	return "okta: " + msg
}

// NotFound reports whether err is an Okta 404. Callers use this to tell
// "already unassigned" apart from a real failure.
func NotFound(err error) bool {
	var ae *APIError
	if ok := asAPIError(err, &ae); ok {
		return ae.Status == http.StatusNotFound
	}
	return false
}

func asAPIError(err error, target **APIError) bool {
	for err != nil {
		if ae, ok := err.(*APIError); ok {
			*target = ae
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

var nextLinkRE = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// do issues one request and decodes the JSON body into out (may be nil).
// It returns the URL of the next page, if the response was paginated.
func (c *Client) do(ctx context.Context, method, path string, body, out any) (string, error) {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return "", fmt.Errorf("encode request: %w", err)
		}
		rdr = bytes.NewReader(buf)
	}

	// path may be a full URL (a pagination "next" link) or an API path.
	u := path
	if !strings.HasPrefix(u, "http") {
		u = c.orgURL + "/api/v1/" + strings.TrimPrefix(path, "/")
	}

	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "SSWS "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Okta returns 429 with an x-rate-limit-reset epoch. One transparent retry
	// keeps bulk operations from dying halfway through a batch.
	if resp.StatusCode == http.StatusTooManyRequests {
		wait := rateLimitWait(resp)
		resp.Body.Close()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}
		return c.do(ctx, method, path, body, out)
	}

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		ae := &APIError{Status: resp.StatusCode}
		_ = json.Unmarshal(raw, ae)
		return "", ae
	}

	var next string
	for _, l := range resp.Header.Values("Link") {
		if m := nextLinkRE.FindStringSubmatch(l); m != nil {
			next = m[1]
		}
	}

	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
			return next, fmt.Errorf("decode response: %w", err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return next, nil
}

func rateLimitWait(resp *http.Response) time.Duration {
	const fallback = 5 * time.Second
	reset := resp.Header.Get("x-rate-limit-reset")
	if reset == "" {
		return fallback
	}
	epoch, err := strconv.ParseInt(reset, 10, 64)
	if err != nil {
		return fallback
	}
	d := time.Until(time.Unix(epoch, 0)) + time.Second
	if d < time.Second {
		return time.Second
	}
	if d > time.Minute {
		return time.Minute
	}
	return d
}

// getAll follows pagination links, appending every page into a single slice.
func getAll[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var all []T
	next := path
	for next != "" {
		var page []T
		n, err := c.do(ctx, http.MethodGet, next, nil, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		next = n
	}
	return all, nil
}

// ---- resource types -------------------------------------------------------

// App is an Okta application.
type App struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	SignOnMode string `json:"signOnMode"`
}

// User is an Okta user.
type User struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Profile struct {
		Login       string `json:"login"`
		Email       string `json:"email"`
		FirstName   string `json:"firstName"`
		LastName    string `json:"lastName"`
		DisplayName string `json:"displayName"`
	} `json:"profile"`
}

// Name renders the user's human-readable name, falling back to the login.
func (u User) Name() string {
	if u.Profile.DisplayName != "" {
		return u.Profile.DisplayName
	}
	n := strings.TrimSpace(u.Profile.FirstName + " " + u.Profile.LastName)
	if n != "" {
		return n
	}
	return u.Profile.Login
}

// Group is an Okta group.
type Group struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Profile struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"profile"`
}

// AppUser is a user's assignment to an app. Scope is "USER" for a direct
// assignment or "GROUP" when the user only has the app via group membership —
// the distinction that decides whether an unassign will actually work.
type AppUser struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	Status      string `json:"status"`
	Created     string `json:"created"`
	LastUpdated string `json:"lastUpdated"`
	Credentials struct {
		UserName string `json:"userName"`
	} `json:"credentials"`
	Profile map[string]any `json:"profile"`
}

// AppGroup is a group's assignment to an app.
type AppGroup struct {
	ID          string         `json:"id"`
	Priority    int            `json:"priority"`
	LastUpdated string         `json:"lastUpdated"`
	Profile     map[string]any `json:"profile"`
}

// ---- read operations ------------------------------------------------------

// Apps lists every app visible to the token.
func (c *Client) Apps(ctx context.Context) ([]App, error) {
	return getAll[App](ctx, c, "apps?limit=200")
}

// App fetches one app by ID.
func (c *Client) App(ctx context.Context, id string) (App, error) {
	var a App
	_, err := c.do(ctx, http.MethodGet, "apps/"+url.PathEscape(id), nil, &a)
	return a, err
}

// Users lists org users. Non-ACTIVE users are included so that assignments
// pointing at suspended or deprovisioned accounts still resolve to a name.
func (c *Client) Users(ctx context.Context) ([]User, error) {
	return getAll[User](ctx, c, "users?limit=200")
}

// User fetches one user by ID or login.
func (c *Client) User(ctx context.Context, idOrLogin string) (User, error) {
	var u User
	_, err := c.do(ctx, http.MethodGet, "users/"+url.PathEscape(idOrLogin), nil, &u)
	return u, err
}

// Groups lists every group in the org.
func (c *Client) Groups(ctx context.Context) ([]Group, error) {
	return getAll[Group](ctx, c, "groups?limit=200")
}

// Group fetches one group by ID.
func (c *Client) Group(ctx context.Context, id string) (Group, error) {
	var g Group
	_, err := c.do(ctx, http.MethodGet, "groups/"+url.PathEscape(id), nil, &g)
	return g, err
}

// GroupMembers lists the users in a group.
func (c *Client) GroupMembers(ctx context.Context, groupID string) ([]User, error) {
	return getAll[User](ctx, c, "groups/"+url.PathEscape(groupID)+"/users?limit=200")
}

// AppUsers lists every user assigned to an app, directly or via a group.
func (c *Client) AppUsers(ctx context.Context, appID string) ([]AppUser, error) {
	return getAll[AppUser](ctx, c, "apps/"+url.PathEscape(appID)+"/users?limit=500")
}

// AppGroups lists every group assigned to an app.
func (c *Client) AppGroups(ctx context.Context, appID string) ([]AppGroup, error) {
	return getAll[AppGroup](ctx, c, "apps/"+url.PathEscape(appID)+"/groups?limit=200")
}

// UserApps lists the apps a user can see, via the appLinks endpoint. This is
// the only per-user reverse view Okta exposes without an admin role.
func (c *Client) UserApps(ctx context.Context, userID string) ([]AppLink, error) {
	return getAll[AppLink](ctx, c, "users/"+url.PathEscape(userID)+"/appLinks")
}

// AppLink is an entry in a user's app dashboard.
type AppLink struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	AppName  string `json:"appName"`
	AppID    string `json:"appInstanceId"`
	LinkURL  string `json:"linkUrl"`
	LogoURL  string `json:"logoUrl"`
	SortKey  int    `json:"sortOrder"`
	Hidden   bool   `json:"hidden"`
	CredType string `json:"credentialsSetup"`
}

// UserGroups lists the groups a user belongs to.
func (c *Client) UserGroups(ctx context.Context, userID string) ([]Group, error) {
	return getAll[Group](ctx, c, "users/"+url.PathEscape(userID)+"/groups?limit=200")
}

// ---- write operations -----------------------------------------------------

// AssignUser adds a direct user assignment to an app.
func (c *Client) AssignUser(ctx context.Context, appID, userID string) (AppUser, error) {
	var out AppUser
	body := map[string]any{"id": userID, "scope": "USER"}
	_, err := c.do(ctx, http.MethodPost, "apps/"+url.PathEscape(appID)+"/users", body, &out)
	return out, err
}

// UnassignUser removes a direct user assignment from an app. It has no effect
// on access the user still holds through an assigned group.
func (c *Client) UnassignUser(ctx context.Context, appID, userID string) error {
	_, err := c.do(ctx, http.MethodDelete,
		"apps/"+url.PathEscape(appID)+"/users/"+url.PathEscape(userID), nil, nil)
	return err
}

// AssignGroup adds a group assignment to an app.
func (c *Client) AssignGroup(ctx context.Context, appID, groupID string) (AppGroup, error) {
	var out AppGroup
	_, err := c.do(ctx, http.MethodPut,
		"apps/"+url.PathEscape(appID)+"/groups/"+url.PathEscape(groupID), map[string]any{}, &out)
	return out, err
}

// UnassignGroup removes a group assignment from an app.
func (c *Client) UnassignGroup(ctx context.Context, appID, groupID string) error {
	_, err := c.do(ctx, http.MethodDelete,
		"apps/"+url.PathEscape(appID)+"/groups/"+url.PathEscape(groupID), nil, nil)
	return err
}

// Me returns the user the API token authenticates as.
func (c *Client) Me(ctx context.Context) (User, error) {
	var u User
	_, err := c.do(ctx, http.MethodGet, "users/me", nil, &u)
	return u, err
}

// Package app wires config, the Okta client and the cache into the single
// context that both the CLI and the TUI operate on.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/term"

	"github.com/keyolk/okx/internal/cache"
	"github.com/keyolk/okx/internal/config"
	"github.com/keyolk/okx/internal/okta"
)

// DefaultTTL is how long a snapshot is considered fresh. Okta directory data
// changes slowly, and explicit refresh remains available whenever current data
// is required.
const DefaultTTL = 7 * 24 * time.Hour

// ErrNoCache is returned in cache-only mode when no snapshot exists.
var ErrNoCache = errors.New("no cached snapshot")

// Context bundles everything a command needs.
type Context struct {
	Cfg          config.Config
	Client       *okta.Client
	Index        *cache.Index
	Path         string
	NeedsRefresh bool
}

// Options control how the context is built.
type Options struct {
	ConfigPath string
	// Refresh forces a fetch even if the cache is fresh.
	Refresh bool
	// TTL overrides DefaultTTL.
	TTL time.Duration
	// AllowStale returns an expired snapshot instead of blocking to replace it.
	AllowStale bool
	// DeferFetch returns an empty snapshot when no cache exists. The caller is
	// responsible for calling Refetch asynchronously.
	DeferFetch bool
	// CacheOnly prohibits network access and returns ErrNoCache when absent.
	CacheOnly bool
	// Quiet suppresses the progress output on stderr.
	Quiet bool
}

// Open loads config and returns a ready context. Its behavior is explicit:
// regular CLI commands synchronously replace an expired/missing snapshot;
// TUI callers use AllowStale+DeferFetch to render immediately and refresh in a
// Bubble Tea command; shell completion uses CacheOnly and never touches the
// network.
func Open(ctx context.Context, opts Options) (*Context, error) {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	client := okta.New(cfg.OrgURL, cfg.Token)
	path := cache.Path(cfg.CacheKey())

	ttl := opts.TTL
	if ttl == 0 {
		ttl = DefaultTTL
	}

	var snap *cache.Snapshot
	if !opts.Refresh {
		snap, err = cache.Load(path)
		if err != nil {
			return nil, fmt.Errorf("load cache: %w", err)
		}
		if snap != nil && snap.OrgURL != cfg.OrgURL {
			snap = nil
		}
	}

	if snap != nil {
		stale := snap.Age() > ttl
		c := &Context{
			Cfg: cfg, Client: client, Index: cache.NewIndex(snap), Path: path,
			NeedsRefresh: stale,
		}
		if !stale || opts.AllowStale || opts.CacheOnly {
			return c, nil
		}
	}

	if opts.CacheOnly {
		return nil, ErrNoCache
	}
	if opts.DeferFetch && !opts.Refresh {
		return &Context{
			Cfg: cfg, Client: client, Index: cache.NewIndex(cache.Empty(cfg.OrgURL)),
			Path: path, NeedsRefresh: true,
		}, nil
	}

	c := &Context{
		Cfg: cfg, Client: client, Index: cache.NewIndex(cache.Empty(cfg.OrgURL)),
		Path: path, NeedsRefresh: true,
	}
	if err := c.Refetch(ctx, opts.Quiet); err != nil {
		return nil, err
	}
	if !opts.Quiet {
		fmt.Fprintf(os.Stderr, "\r\033[Kfetched %d apps, %d users, %d groups\n",
			len(c.Index.Apps), len(c.Index.Users), len(c.Index.Groups))
	}
	return c, nil
}

// Invalidate drops the on-disk cache so the next Open refetches. Called after
// any write, since a mutation makes the snapshot wrong immediately.
func (c *Context) Invalidate() error {
	if err := os.Remove(c.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove cache: %w", err)
	}
	return nil
}

// FetchIndex pulls and persists a fresh snapshot without mutating Context. TUI
// commands use this so Bubble Tea owns all model state changes in Update.
func (c *Context) FetchIndex(ctx context.Context, quiet bool) (*cache.Index, error) {
	snap, err := cache.Fetch(ctx, c.Client, c.Cfg.OrgURL, progressWriter(quiet))
	if err != nil {
		return nil, err
	}
	if err := cache.Save(c.Path, snap); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write cache: %v\n", err)
	}
	return cache.NewIndex(snap), nil
}

// Refetch pulls a fresh snapshot and replaces the in-process index.
func (c *Context) Refetch(ctx context.Context, quiet bool) error {
	index, err := c.FetchIndex(ctx, quiet)
	if err != nil {
		return err
	}
	c.Index = index
	c.NeedsRefresh = false
	return nil
}

func progressWriter(quiet bool) cache.Progress {
	if quiet || !isTerminal(os.Stderr) {
		return nil
	}
	return func(stage string, done, total int) {
		if total > 0 {
			fmt.Fprintf(os.Stderr, "\r\033[Kfetching %s… %d/%d", stage, done, total)
		} else {
			fmt.Fprintf(os.Stderr, "\r\033[Kfetching %s…", stage)
		}
	}
}

func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

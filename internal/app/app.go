// Package app wires config, the Okta client and the cache into the single
// context that both the CLI and the TUI operate on.
package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/term"

	"github.com/keyolk/okx/internal/cache"
	"github.com/keyolk/okx/internal/config"
	"github.com/keyolk/okx/internal/okta"
)

// DefaultTTL is how long a snapshot is considered fresh enough to use without
// comment. Assignments change on human timescales, not machine ones.
const DefaultTTL = 15 * time.Minute

// Context bundles everything a command needs.
type Context struct {
	Cfg    config.Config
	Client *okta.Client
	Index  *cache.Index
	Path   string
}

// Options control how the context is built.
type Options struct {
	ConfigPath string
	// Refresh forces a fetch even if the cache is fresh.
	Refresh bool
	// TTL overrides DefaultTTL.
	TTL time.Duration
	// Quiet suppresses the progress output on stderr.
	Quiet bool
}

// Open loads config, restores or refreshes the cache, and returns a ready
// context.
func Open(ctx context.Context, opts Options) (*Context, error) {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	client := okta.New(cfg.OrgURL, cfg.Token)
	path := cache.Path(cfg.OrgName())

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
			snap = nil // cache belongs to a different org
		}
		if snap != nil && snap.Age() > ttl {
			snap = nil
		}
	}

	if snap == nil {
		progress := progressWriter(opts.Quiet)
		snap, err = cache.Fetch(ctx, client, cfg.OrgURL, progress)
		if err != nil {
			return nil, err
		}
		if err := cache.Save(path, snap); err != nil {
			// A cache we cannot persist is a performance problem, not a
			// correctness one — warn and carry on with the in-memory copy.
			fmt.Fprintf(os.Stderr, "warning: could not write cache: %v\n", err)
		}
		if !opts.Quiet {
			fmt.Fprintf(os.Stderr, "\r\033[Kfetched %d apps, %d users, %d groups\n",
				len(snap.Apps), len(snap.Users), len(snap.Groups))
		}
	}

	return &Context{Cfg: cfg, Client: client, Index: cache.NewIndex(snap), Path: path}, nil
}

// Invalidate drops the on-disk cache so the next Open refetches. Called after
// any write, since a mutation makes the snapshot wrong immediately.
func (c *Context) Invalidate() {
	_ = os.Remove(c.Path)
}

// Refetch pulls a fresh snapshot and replaces the in-process index.
func (c *Context) Refetch(ctx context.Context, quiet bool) error {
	snap, err := cache.Fetch(ctx, c.Client, c.Cfg.OrgURL, progressWriter(quiet))
	if err != nil {
		return err
	}
	if err := cache.Save(c.Path, snap); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write cache: %v\n", err)
	}
	c.Index = cache.NewIndex(snap)
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

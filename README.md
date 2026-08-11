# okx — Okta app assignment manager

A CLI and TUI for managing Okta application assignments (users and groups) for
apps you administer. Works against any Okta org whose API token you hold.

## Setup

okx reads the same `~/.okta/okta.yaml` the official `okta` CLI uses:

```yaml
okta:
  client:
    orgUrl: 'https://your-org.okta.com'
    token: <SSWS token>
```

Or set `OKTA_ORG_URL` and `OKTA_API_TOKEN`. Then:

```
make install   # builds and copies to ~/.local/bin/okx
```

## CLI

```
okx                          # open the TUI (default)
okx apps [filter]            # list apps
okx search <query>           # search apps, groups, and members together
okx show <app>               # who has the app, and why (direct / group / both)
okx show <app> --groups      # list the assigned groups
okx show <app> --csv         # CSV export
okx user <user>              # which apps a user has, and why
okx group <group>            # group members + which apps it grants
okx assign <app> <user>...   # assign users (fuzzy match on login/name/email)
okx assign <app> <group>... --group
okx unassign <app> <user>...
okx unassign <app> <group>... --group
okx whoami                   # token identity + visible scope
okx refresh                  # refetch the snapshot (`refetch` alias also works)
```

Every target resolves by exact ID, exact name, or unique fuzzy match; an
ambiguous query lists the candidates instead of guessing.

### Safety

- All writes preview the planned changes first.
- `--dry-run` / `-n` previews without applying.
- Interactive prompts default to **no**; non-interactive stdin refuses unless
  `--yes` / `-y` is passed.
- Removing a direct user assignment that also has group access is flagged —
  the unassign won't actually revoke access.

### Shell completion

```
okx completion zsh > ~/.zsh/completions/_okx   # or bash/fish
```

Completion is fuzzy-filtered over the cached snapshot, so run `okx refresh`
once after setup.

## TUI

The default view is **1 Apps**, with **2 Groups** and **3 Users** as peer
resource views. Press `1`, `2`, or `3` from any screen to switch immediately.

- Apps drill down to assignments; Groups to granted apps; Users to app access
- `j/k` move, `enter` drill in, `esc`/`h` back, `q` quit
- `/` fuzzy-filters the current resource view; `R` refetches; `?` opens help
- On an app: `tab` switches between users and groups
- `a` opens a fuzzy multi-select user picker; `A` the group picker
- `d` removes the selected assignment (with confirmation)
- `D` / `G` markers distinguish direct vs group-derived access

## Cache

The first run snapshots apps, users, groups, app assignments and group
members into `~/Library/Caches/okx/<org>-<token-digest>.json`. The default TTL
is 7 days; use `--ttl` to override it, `--refresh` on any command to refetch
first, or `okx refresh` / `okx refetch` explicitly. The TUI renders stale data
immediately while refreshing it in the background. The snapshot is invalidated
after any write. Okta rate-limits `/apps` at 50 req/min, so the cache is what
makes the TUI responsive.

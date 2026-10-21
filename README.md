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

Or set `OKTA_ORG_URL` and `OKTA_API_TOKEN`.

In the TUI, user, group and app names link to their admin console pages
(cmd/ctrl+click, or `o` on the selected row). The admin console URL is derived
for Okta-hosted orgs (`https://acme.okta.com` → `https://acme-admin.okta.com`,
likewise `oktapreview.com` and `okta-emea.com`). With a custom domain okx
cannot know it, so set it, or there are no links:

```yaml
okta:
  client:
    adminUrl: 'https://your-org-admin.okta.com'
```

`OKTA_ADMIN_URL` overrides it. `okx whoami` prints the URL in use. Behind
tmux, links need `set -as terminal-features '*:hyperlinks'` (tmux 3.4+).

Then:

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
- `:` opens a command palette of every action the current screen can run
- On an app: `tab` switches between users and groups
- `a` assigns to the shown tab (users or groups) through a fuzzy multi-select
  picker; `A` always opens the group picker
- `d` removes the selected assignment (confirm with `y`)
- `D` / `G` markers distinguish direct vs group-derived access
- `o` opens the selected user, group or app in the Okta admin console; the
  names are also hyperlinks (see Setup for `adminUrl`)

The footer, `?` help and the palette are all generated from one key table, so
they always match what the keys do. Rebind any action in
`~/.config/okx/keys.yaml` (`$XDG_CONFIG_HOME` honoured); `?` lists the
actions, and `context.action` rebinds one context only:

```yaml
list.down: [j, down, ctrl+n]
refresh: [R, ctrl+r]
```

A file that would hide quit/help or bind one key twice is rejected with a
message, and the defaults stay. The last tab, each tab's filter and selected
row are kept in `~/.local/state/okx/ui.json` (`$XDG_STATE_HOME`).

## Cache

The first run snapshots apps, users, groups, app assignments and group
members into `~/Library/Caches/okx/<org>-<token-digest>.json`. The default TTL
is 7 days; use `--ttl` to override it, `--refresh` on any command to refetch
first, or `okx refresh` / `okx refetch` explicitly. The TUI renders stale data
immediately while refreshing it in the background. The snapshot is invalidated
after any write. Okta rate-limits `/apps` at 50 req/min, so the cache is what
makes the TUI responsive.

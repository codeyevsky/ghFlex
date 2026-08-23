# gh-autofollow

A small command line tool that walks a GitHub list page (followers, following,
stargazers or watchers) and follows everyone on it, then moves on to the next
page. It drives a real browser through Playwright, so it uses your normal logged
in session and does not touch the GitHub API tokens. Works with Firefox and
Chromium.

## Why a browser instead of the API

The GitHub REST API is the obvious way to follow users, but it needs a personal
access token with `user:follow` scope and it is easy to trip the abuse limits.
Driving the same follow button a person would click keeps everything tied to the
normal web session and makes the behaviour easy to watch while it runs.

## Install

```bash
git clone git@github.com:codeyevsky/gh-autofollow.git
cd gh-autofollow
npm install
npx playwright install firefox chromium
```

## Usage

```bash
# log in once, the session is saved in a dedicated profile
node src/cli.js login --browser firefox

# dry run first, prints who would be followed without touching anything
node src/cli.js run --url torvalds:followers --pages 3 --max 30 --dry-run

# real run
node src/cli.js run --url torvalds:followers --pages 20 --max 300

# see what it did
node src/cli.js stats
```

The `--url` argument takes a full GitHub URL or one of these shortcuts:
`user:followers`, `user:following`, `owner/repo:stargazers`,
`owner/repo:watchers`.

| Option | Default | Meaning |
|--------|---------|---------|
| `--browser` | `firefox` | `firefox` or `chromium` |
| `--pages` | 3 | how many pages to walk |
| `--max` | 30 | total follow cap for the run |
| `--min-delay` / `--max-delay` | 4000 / 9000 | random wait in ms between follows |
| `--page-delay` | 6000 | wait in ms between pages |
| `--dry-run` | off | list targets, follow nobody |
| `--headless` | off | hide the browser window |
| `--system-chromium` | off | use `/usr/bin/chromium` |
| `--use-my-profile` | off | reuse your real Chromium profile (Chromium only) |

## How it works

Each follower row on GitHub contains both a follow form and an unfollow form, and
only one is shown at a time. The tool reads the visible follow forms, which is the
reliable signal for "not following yet", and submits each one with a POST that
carries the form's authenticity token. Clicking the button directly is not
enough, GitHub does not always submit the form on a synthetic click, so the POST
is sent explicitly. Pagination follows the Next link by navigating to its href.

## State

Everything is kept under `~/.local/share/gh-autofollow/`:

- `profiles/firefox` and `profiles/chromium` hold the browser sessions
- `state.json` is a log of follows, skips and past runs
- `blocklist.txt` is an optional list, one username per line, that is never touched

## Authentication

By default the tool runs its own browser profile, so you log in once with the
`login` command and the session is remembered after that. With `--use-my-profile`
on Chromium it drives your existing Chromium profile instead and reuses the login
already there; close every Chromium window first, otherwise the profile is locked.

## Rate limits

Bulk following is exactly the kind of pattern GitHub watches for. The tool waits a
random few seconds between follows and stops as soon as a response looks throttled
(HTTP 429 or 403, or a secondary rate limit message). Lowering the delays and
raising `--max` makes a limit or a temporary block more likely, so start small.

## Tests

```bash
node test/fixture.test.mjs
```

## Note

Automated following can violate GitHub's Acceptable Use Policies. Use it on your
own account and at your own risk.

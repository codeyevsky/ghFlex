package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func dataBase() string {
	if base := os.Getenv("XDG_DATA_HOME"); base != "" {
		return base
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

// DataDir resolves where state lives: $GITHUBFLEX_HOME or
// <data home>/githubflex. If a pre-rename gh-autofollow directory could not
// be migrated (see MigrateData), it keeps being used so old state carries
// over.
func DataDir() string {
	if d := os.Getenv("GITHUBFLEX_HOME"); d != "" {
		return d
	}
	if d := os.Getenv("GH_AUTOFOLLOW_HOME"); d != "" {
		return d
	}
	oldDir := filepath.Join(dataBase(), "gh-autofollow")
	newDir := filepath.Join(dataBase(), "githubflex")
	if _, err := os.Stat(oldDir); err == nil {
		if _, err := os.Stat(newDir); err != nil {
			return oldDir
		}
	}
	return newDir
}

// MigrateData renames the old gh-autofollow data directory to githubflex, so
// state, sessions and blocklist carry over under the new name.
func MigrateData() {
	if os.Getenv("GITHUBFLEX_HOME") != "" || os.Getenv("GH_AUTOFOLLOW_HOME") != "" {
		return
	}
	oldDir := filepath.Join(dataBase(), "gh-autofollow")
	newDir := filepath.Join(dataBase(), "githubflex")
	if _, err := os.Stat(oldDir); err != nil {
		return
	}
	if _, err := os.Stat(newDir); err == nil {
		return
	}
	_ = os.Rename(oldDir, newDir)
}

type Entry struct {
	At     string `json:"at"`
	From   string `json:"from,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type Run struct {
	At      string `json:"at"`
	URL     string `json:"url"`
	Mode    string `json:"mode"`
	Count   int    `json:"count"`
	Pages   int    `json:"pages"`
	Stopped string `json:"stopped"`
	DryRun  bool   `json:"dryRun"`
}

// Old (Node) state files stored the count under a per-mode key like
// "followed": 2 instead of "count", so accept both.
func (r *Run) UnmarshalJSON(b []byte) error {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	str := func(k string) string { s, _ := m[k].(string); return s }
	r.At, r.URL, r.Mode, r.Stopped = str("at"), str("url"), str("mode"), str("stopped")
	r.DryRun, _ = m["dryRun"].(bool)
	if p, ok := m["pages"].(float64); ok {
		r.Pages = int(p)
	}
	for _, k := range []string{"count", "followed", "unfollowed", "starred", "unstarred"} {
		if n, ok := m[k].(float64); ok {
			r.Count = int(n)
			break
		}
	}
	return nil
}

type State struct {
	Followed map[string]Entry `json:"followed"`
	Starred  map[string]Entry `json:"starred"`
	Skipped  map[string]Entry `json:"skipped"`
	Runs     []Run            `json:"runs"`
}

func statePath() string { return filepath.Join(DataDir(), "state.json") }

func LoadState() *State {
	s := &State{
		Followed: map[string]Entry{},
		Starred:  map[string]Entry{},
		Skipped:  map[string]Entry{},
	}
	b, err := os.ReadFile(statePath())
	if err == nil {
		_ = json.Unmarshal(b, s)
	}
	if s.Followed == nil {
		s.Followed = map[string]Entry{}
	}
	if s.Starred == nil {
		s.Starred = map[string]Entry{}
	}
	if s.Skipped == nil {
		s.Skipped = map[string]Entry{}
	}
	return s
}

func (s *State) Save() error {
	if err := os.MkdirAll(DataDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(), b, 0o644)
}

func ProfilePath(browser string) string {
	p := filepath.Join(DataDir(), "profiles", browser)
	_ = os.MkdirAll(p, 0o755)
	return p
}

// Blocklist reads blocklist.txt: one user or owner/repo per line, # comments.
func Blocklist() map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(filepath.Join(DataDir(), "blocklist.txt"))
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.ToLower(strings.TrimSpace(l))
		if l != "" && !strings.HasPrefix(l, "#") {
			out[l] = true
		}
	}
	return out
}

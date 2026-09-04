package main

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// Rows/cards whose form for the given verb is visible are picked, hidden ones
// are skipped: alice/carol have a visible follow form, bob a visible unfollow
// form; octo/rocket a visible star form, octo/comet a visible unstar form.
func TestFixtureVisibleOnly(t *testing.T) {
	t.Setenv("GITHUBFLEX_HOME", t.TempDir())

	pw, err := playwright.Run()
	if err != nil {
		t.Skipf("playwright driver not installed (run: ghflex install): %v", err)
	}
	defer pw.Stop()
	b, err := pw.Firefox.Launch()
	if err != nil {
		t.Skipf("firefox not installed (run: ghflex install): %v", err)
	}
	defer b.Close()
	page, err := b.NewPage()
	if err != nil {
		t.Fatal(err)
	}

	abs, err := filepath.Abs(filepath.Join("testdata", "page1.html"))
	if err != nil {
		t.Fatal(err)
	}
	url := "file://" + abs

	want := map[string][]string{
		"follow":   {"alice", "carol"},
		"unfollow": {"bob"},
		"star":     {"octo/rocket"},
		"unstar":   {"octo/comet"},
	}
	for _, mode := range []string{"follow", "unfollow", "star", "unstar"} {
		stats, err := RunAction(page, RunOptions{
			URL: url, Mode: mode, MaxPages: 1, MaxActions: 10,
			MinDelay: 0, MaxDelay: 0, PageDelay: 0, DryRun: true,
			Log: func(string) {},
		})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if !reflect.DeepEqual(stats.Done, want[mode]) {
			t.Errorf("%s: got %v, want %v", mode, stats.Done, want[mode])
		}
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"torvalds:followers":   "https://github.com/torvalds?tab=followers",
		"torvalds:stars":       "https://github.com/torvalds?tab=stars",
		"me:following":         "https://github.com/me?tab=following",
		"golang/go:stargazers": "https://github.com/golang/go/stargazers",
		"golang/go":            "https://github.com/golang/go/stargazers",
		"torvalds":             "https://github.com/torvalds?tab=followers",
		"https://github.com/x": "https://github.com/x",
	}
	for in, out := range cases {
		if got := normalizeURL(in); got != out {
			t.Errorf("normalizeURL(%q) = %q, want %q", in, got, out)
		}
	}
}

func TestJitterBounds(t *testing.T) {
	for i := 0; i < 100; i++ {
		d := jitter(4*time.Millisecond, 9*time.Millisecond)
		if d < 4*time.Millisecond || d > 9*time.Millisecond {
			t.Fatalf("jitter out of bounds: %v", d)
		}
	}
	if jitter(5*time.Millisecond, 5*time.Millisecond) != 5*time.Millisecond {
		t.Fatal("jitter with equal bounds should return min")
	}
}

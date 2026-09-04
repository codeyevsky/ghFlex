package main

import (
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

var rateLimitHints = []string{
	"secondary rate limit",
	"abuse detection",
	"have been rate limited",
	"whoa there",
	"too many requests",
}

type ModeSpec struct {
	Kind  string // "user" or "repo"
	Past  string // followed / unfollowed / starred / unstarred
	Mark  string
	Color string // tint code for the mark
}

// Every mode the engine knows. Kind decides which collector runs and which
// state bucket the result lands in.
var Modes = map[string]ModeSpec{
	"follow":   {Kind: "user", Past: "followed", Mark: "[+]", Color: cGreen},
	"unfollow": {Kind: "user", Past: "unfollowed", Mark: "[-]", Color: cRed},
	"star":     {Kind: "repo", Past: "starred", Mark: "[*]", Color: cYellow},
	"unstar":   {Kind: "repo", Past: "unstarred", Mark: "[x]", Color: cLilac},
}

type target struct {
	Name   string
	Action string
	Token  string
}

// A follower/following row carries both a follow and an unfollow form; GitHub
// hides one with CSS. The visible one tells us the current state.
const collectUsersJS = `(verb) => {
  const vis = (el) => el && el.offsetParent !== null && getComputedStyle(el).display !== 'none';
  const isHeader = (el) =>
    !!el.closest('header, [class*="ProfileHeader"], .h-card, .vcard, [itemtype*="Person"]');
  const out = [];
  for (const form of document.querySelectorAll('form[action^="/users/' + verb + '?target="]')) {
    if (!vis(form) || isHeader(form)) continue;
    const m = form.getAttribute('action').match(/target=([^&]+)/);
    if (!m) continue;
    const name = decodeURIComponent(m[1]);
    const token = form.querySelector('input[name="authenticity_token"]')?.value;
    if (!token) continue;
    out.push({ name, action: form.action, token });
  }
  return out;
}`

// A repo card on a stars listing works the same way: paired /owner/repo/star
// and /owner/repo/unstar forms, the visible one showing the current state.
const collectReposJS = `(verb) => {
  const vis = (el) => el && el.offsetParent !== null && getComputedStyle(el).display !== 'none';
  const out = [];
  for (const form of document.querySelectorAll('form[action$="/star"], form[action$="/unstar"]')) {
    if (!vis(form)) continue;
    const a = (form.getAttribute('action') || '').replace(/^https?:\/\/[^/]+/, '').split('?')[0];
    const m = a.match(/^\/([^/]+)\/([^/]+)\/(star|unstar)$/);
    if (!m || m[3] !== verb) continue;
    const token = form.querySelector('input[name="authenticity_token"]')?.value;
    if (!token) continue;
    out.push({ name: m[1] + '/' + m[2], action: form.action, token });
  }
  return out;
}`

// Send the action the same way GitHub's own button does: a POST to the form
// action with the page's authenticity token, reusing the session cookies.
const submitJS = `async ({ action, token }) => {
  try {
    const body = new URLSearchParams();
    body.set('authenticity_token', token);
    const r = await fetch(action, {
      method: 'POST',
      body,
      headers: { Accept: 'text/html', 'X-Requested-With': 'XMLHttpRequest' },
      credentials: 'same-origin',
    });
    const text = await r.text();
    return { status: r.status, ok: r.ok, text: text.slice(0, 500) };
  } catch (e) {
    return { status: 0, ok: false, text: String(e) };
  }
}`

const nextPageJS = `() => {
  const links = [...document.querySelectorAll('.pagination a, .paginate-container a, a[rel="next"]')];
  const next = links.find(
    (a) => /next/i.test(a.textContent || '') && a.getAttribute('aria-disabled') !== 'true'
  );
  return next ? next.href : null;
}`

func collectTargets(page playwright.Page, skip map[string]bool, verb, kind string) ([]target, error) {
	js := collectUsersJS
	if kind == "repo" {
		js = collectReposJS
	}
	raw, err := page.Evaluate(js, verb)
	if err != nil {
		return nil, err
	}
	var out []target
	seen := map[string]bool{}
	list, _ := raw.([]any)
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		t := target{}
		t.Name, _ = m["name"].(string)
		t.Action, _ = m["action"].(string)
		t.Token, _ = m["token"].(string)
		k := strings.ToLower(t.Name)
		if t.Name == "" || skip[k] || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, t)
	}
	return out, nil
}

type submitResult struct {
	Status int
	OK     bool
	Text   string
}

func submit(page playwright.Page, t target) submitResult {
	raw, err := page.Evaluate(submitJS, map[string]any{"action": t.Action, "token": t.Token})
	if err != nil {
		return submitResult{Status: 0, OK: false, Text: err.Error()}
	}
	m, _ := raw.(map[string]any)
	res := submitResult{}
	if s, ok := m["status"].(float64); ok {
		res.Status = int(s)
	} else if s, ok := m["status"].(int); ok {
		res.Status = s
	}
	res.OK, _ = m["ok"].(bool)
	res.Text, _ = m["text"].(string)
	return res
}

func rateLimitReason(res submitResult) string {
	if res.Status == 429 || res.Status == 403 || res.Status == 422 {
		return fmt.Sprintf("HTTP %d (limit / throttled)", res.Status)
	}
	t := strings.ToLower(res.Text)
	for _, h := range rateLimitHints {
		if strings.Contains(t, h) {
			return h
		}
	}
	return ""
}

func gotoNextPage(page playwright.Page) (bool, error) {
	// Clicking the Next link does not always navigate, so read its href and go there.
	raw, err := page.Evaluate(nextPageJS)
	if err != nil {
		return false, err
	}
	href, _ := raw.(string)
	if href == "" {
		return false, nil
	}
	if _, err := page.Goto(href, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		return false, err
	}
	page.WaitForTimeout(800)
	return true, nil
}

type RunOptions struct {
	URL        string
	Mode       string // follow | unfollow | star | unstar
	MaxPages   int
	MaxActions int
	MinDelay   time.Duration
	MaxDelay   time.Duration
	PageDelay  time.Duration
	DryRun     bool
	Log        func(string)
}

type RunStats struct {
	Done    []string
	Skipped int
	Pages   int
	Stopped string
	Mode    string
}

func jitter(min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	return min + time.Duration(rand.Int63n(int64(max-min)))
}

type TreeOptions struct {
	RootUser     string
	MaxDepth     int // 0 = only the root user's stars
	PagesPerUser int
	MaxActions   int
	MinDelay     time.Duration
	MaxDelay     time.Duration
	PageDelay    time.Duration
	DryRun       bool
	Log          func(string)
}

// RunStarTree stars the repos RootUser starred, then branches: every starred
// repo's owner is queued and their stars page is walked too, breadth-first,
// until MaxDepth or MaxActions is reached. Orgs (which cannot star) simply
// yield an empty stars page and end that branch.
func RunStarTree(page playwright.Page, o TreeOptions) (*RunStats, error) {
	log := o.Log
	if log == nil {
		log = func(s string) { fmt.Println(s) }
	}
	state := LoadState()
	skip := Blocklist()
	stats := &RunStats{Mode: "startree"}
	visited := map[string]bool{strings.ToLower(o.RootUser): true}
	type node struct {
		user  string
		depth int
	}
	queue := []node{{o.RootUser, 0}}
	consecutiveFail := 0

	for len(queue) > 0 && stats.Stopped == "" {
		if len(stats.Done) >= o.MaxActions {
			stats.Stopped = "reached max stars"
			break
		}
		cur := queue[0]
		queue = queue[1:]
		indent := strings.Repeat("    ", cur.depth)
		url := fmt.Sprintf("https://github.com/%s?tab=stars", cur.user)
		log(indent + "  " + tint(cPurple, fmt.Sprintf("-> depth %d: stars of %s", cur.depth, cur.user)))
		if _, err := page.Goto(url, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		}); err != nil {
			log(indent + "     ! " + err.Error())
			continue
		}
		stats.Pages++ // counts users actually visited

		for p := 1; p <= o.PagesPerUser; p++ {
			targets, err := collectTargets(page, skip, "star", "repo")
			if err != nil {
				return nil, err
			}
			if len(targets) == 0 {
				log(indent + "     (nothing to star here)")
			}
			for _, t := range targets {
				if len(stats.Done) >= o.MaxActions {
					break
				}
				skip[strings.ToLower(t.Name)] = true

				owner := strings.SplitN(t.Name, "/", 2)[0]
				if ol := strings.ToLower(owner); cur.depth < o.MaxDepth && !visited[ol] {
					visited[ol] = true
					queue = append(queue, node{owner, cur.depth + 1})
				}

				if o.DryRun {
					log(fmt.Sprintf("%s     %s would star %s", indent, tint(cCyan, "[dry-run]"), t.Name))
					stats.Done = append(stats.Done, t.Name)
					continue
				}

				res := submit(page, t)
				if reason := rateLimitReason(res); reason != "" {
					stats.Stopped = "rate limited (" + reason + ")"
					log(indent + "     " + tint(cRed, "! stopping: "+stats.Stopped))
					break
				}
				now := time.Now().UTC().Format(time.RFC3339)
				if res.OK {
					consecutiveFail = 0
					stats.Done = append(stats.Done, t.Name)
					state.Starred[t.Name] = Entry{At: now, From: url}
					_ = state.Save()
					log(fmt.Sprintf("%s     %s %s  (%d/%d)", indent, tint(cYellow, "[*]"), t.Name, len(stats.Done), o.MaxActions))
				} else {
					stats.Skipped++
					state.Skipped[t.Name] = Entry{At: now, Reason: fmt.Sprintf("HTTP %d", res.Status)}
					_ = state.Save()
					log(fmt.Sprintf("%s     ? %s: HTTP %d - skipped", indent, t.Name, res.Status))
					if consecutiveFail++; consecutiveFail >= 5 {
						stats.Stopped = fmt.Sprintf("stopped: %d failures in a row (likely hit a limit)", consecutiveFail)
						log(indent + "     " + tint(cRed, "! "+stats.Stopped))
						break
					}
				}
				time.Sleep(jitter(o.MinDelay, o.MaxDelay))
			}

			if stats.Stopped != "" || len(stats.Done) >= o.MaxActions || p == o.PagesPerUser {
				break
			}
			time.Sleep(o.PageDelay)
			more, err := gotoNextPage(page)
			if err != nil {
				return nil, err
			}
			if !more {
				break
			}
		}
	}

	if stats.Stopped == "" {
		if len(stats.Done) >= o.MaxActions {
			stats.Stopped = "reached max stars"
		} else {
			stats.Stopped = "tree exhausted"
		}
	}
	state.Runs = append(state.Runs, Run{
		At:      time.Now().UTC().Format(time.RFC3339),
		URL:     "startree:" + o.RootUser,
		Mode:    "startree",
		Count:   len(stats.Done),
		Pages:   stats.Pages,
		Stopped: stats.Stopped,
		DryRun:  o.DryRun,
	})
	_ = state.Save()
	return stats, nil
}

func RunAction(page playwright.Page, o RunOptions) (*RunStats, error) {
	spec, ok := Modes[o.Mode]
	if !ok {
		return nil, fmt.Errorf("unknown mode: %s", o.Mode)
	}
	log := o.Log
	if log == nil {
		log = func(s string) { fmt.Println(s) }
	}

	state := LoadState()
	skip := Blocklist()
	if spec.Kind == "user" {
		if u, err := url.Parse(o.URL); err == nil {
			if segs := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' }); len(segs) > 0 {
				skip[strings.ToLower(segs[0])] = true // profile owner
			}
		}
	}

	stats := &RunStats{Mode: o.Mode}
	consecutiveFail := 0

	log("  " + tint(cPurple, fmt.Sprintf("-> %s: %s", o.Mode, o.URL)))
	if _, err := page.Goto(o.URL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		return nil, err
	}

	for p := 1; p <= o.MaxPages; p++ {
		stats.Pages = p
		log("\n  " + tint(cDim, fmt.Sprintf("-- page %d -- %s", p, page.URL())))

		targets, err := collectTargets(page, skip, o.Mode, spec.Kind)
		if err != nil {
			return nil, err
		}
		if len(targets) == 0 {
			log(fmt.Sprintf("   (nothing to %s on this page)", o.Mode))
		}

		for _, t := range targets {
			if len(stats.Done) >= o.MaxActions {
				break
			}
			skip[strings.ToLower(t.Name)] = true

			if o.DryRun {
				log(fmt.Sprintf("   %s would %s %s", tint(cCyan, "[dry-run]"), o.Mode, t.Name))
				stats.Done = append(stats.Done, t.Name)
				continue
			}

			res := submit(page, t)
			if reason := rateLimitReason(res); reason != "" {
				stats.Stopped = fmt.Sprintf("rate limited (%s)", reason)
				log("   " + tint(cRed, "! stopping: "+stats.Stopped))
				break
			}

			now := time.Now().UTC().Format(time.RFC3339)
			if res.OK {
				consecutiveFail = 0
				stats.Done = append(stats.Done, t.Name)
				switch o.Mode {
				case "follow":
					state.Followed[t.Name] = Entry{At: now, From: o.URL}
				case "unfollow":
					delete(state.Followed, t.Name)
				case "star":
					state.Starred[t.Name] = Entry{At: now, From: o.URL}
				case "unstar":
					delete(state.Starred, t.Name)
				}
				_ = state.Save()
				log(fmt.Sprintf("   %s %s  (%d/%d)", tint(spec.Color, spec.Mark), t.Name, len(stats.Done), o.MaxActions))
			} else {
				stats.Skipped++
				state.Skipped[t.Name] = Entry{At: now, Reason: fmt.Sprintf("HTTP %d", res.Status)}
				_ = state.Save()
				log(fmt.Sprintf("   ? %s: HTTP %d - skipped", t.Name, res.Status))
				consecutiveFail++
				if consecutiveFail >= 5 {
					stats.Stopped = fmt.Sprintf("stopped: %d failures in a row (likely hit a limit)", consecutiveFail)
					log("   " + tint(cRed, "! "+stats.Stopped))
					break
				}
			}

			time.Sleep(jitter(o.MinDelay, o.MaxDelay))
		}

		if stats.Stopped != "" {
			break
		}
		if len(stats.Done) >= o.MaxActions {
			stats.Stopped = "reached --max"
			break
		}
		log(fmt.Sprintf("   page done (%d total so far)", len(stats.Done)))

		if p == o.MaxPages {
			stats.Stopped = "reached --pages"
			break
		}
		time.Sleep(o.PageDelay)
		more, err := gotoNextPage(page)
		if err != nil {
			return nil, err
		}
		if !more {
			stats.Stopped = "no more pages"
			break
		}
	}

	state.Runs = append(state.Runs, Run{
		At:      time.Now().UTC().Format(time.RFC3339),
		URL:     o.URL,
		Mode:    o.Mode,
		Count:   len(stats.Done),
		Pages:   stats.Pages,
		Stopped: stats.Stopped,
		DryRun:  o.DryRun,
	})
	_ = state.Save()
	return stats, nil
}

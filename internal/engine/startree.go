package engine

import (
	"fmt"
	"strings"
	"time"
	"github.com/codeyevsky/ghFlex/internal/style"
	"github.com/mxschmitt/playwright-go"
)

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
		log(indent + "  " + style.Tint(style.Purple, fmt.Sprintf("-> depth %d: stars of %s", cur.depth, cur.user)))
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
					log(fmt.Sprintf("%s     %s would star %s", indent, style.Tint(style.Cyan, "[dry-run]"), t.Name))
					stats.Done = append(stats.Done, t.Name)
					continue
				}

				res := submit(page, t)
				if reason := rateLimitReason(res); reason != "" {
					stats.Stopped = "rate limited (" + reason + ")"
					log(indent + "     " + style.Tint(style.Red, "! stopping: "+stats.Stopped))
					break
				}
				now := time.Now().UTC().Format(time.RFC3339)
				if res.OK {
					consecutiveFail = 0
					stats.Done = append(stats.Done, t.Name)
					state.Starred[t.Name] = Entry{At: now, From: url}
					_ = state.Save()
					log(fmt.Sprintf("%s     %s %s  (%d/%d)", indent, style.Tint(style.Yellow, "[*]"), t.Name, len(stats.Done), o.MaxActions))
				} else {
					stats.Skipped++
					state.Skipped[t.Name] = Entry{At: now, Reason: fmt.Sprintf("HTTP %d", res.Status)}
					_ = state.Save()
					log(fmt.Sprintf("%s     ? %s: HTTP %d - skipped", indent, t.Name, res.Status))
					if consecutiveFail++; consecutiveFail >= 5 {
						stats.Stopped = fmt.Sprintf("stopped: %d failures in a row (likely hit a limit)", consecutiveFail)
						log(indent + "     " + style.Tint(style.Red, "! "+stats.Stopped))
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
package engine

import (
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"time"
	"github.com/codeyevsky/ghFlex/internal/style"
	"github.com/mxschmitt/playwright-go"
)

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

// RunAction walks the list page(s) at o.URL and applies o.Mode to every visible
// entry, one page at a time, spacing actions by the configured delays and
// stopping on a rate-limit response or a run of failures.
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

	log("  " + style.Tint(style.Purple, fmt.Sprintf("-> %s: %s", o.Mode, o.URL)))
	if _, err := page.Goto(o.URL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		return nil, err
	}

	for p := 1; p <= o.MaxPages; p++ {
		stats.Pages = p
		log("\n  " + style.Tint(style.Dim, fmt.Sprintf("-- page %d -- %s", p, page.URL())))

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
				log(fmt.Sprintf("   %s would %s %s", style.Tint(style.Cyan, "[dry-run]"), o.Mode, t.Name))
				stats.Done = append(stats.Done, t.Name)
				continue
			}

			res := submit(page, t)
			if reason := rateLimitReason(res); reason != "" {
				stats.Stopped = fmt.Sprintf("rate limited (%s)", reason)
				log("   " + style.Tint(style.Red, "! stopping: "+stats.Stopped))
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
				log(fmt.Sprintf("   %s %s  (%d/%d)", style.Tint(spec.Color, spec.Mark), t.Name, len(stats.Done), o.MaxActions))
			} else {
				stats.Skipped++
				state.Skipped[t.Name] = Entry{At: now, Reason: fmt.Sprintf("HTTP %d", res.Status)}
				_ = state.Save()
				log(fmt.Sprintf("   ? %s: HTTP %d - skipped", t.Name, res.Status))
				consecutiveFail++
				if consecutiveFail >= 5 {
					stats.Stopped = fmt.Sprintf("stopped: %d failures in a row (likely hit a limit)", consecutiveFail)
					log("   " + style.Tint(style.Red, "! "+stats.Stopped))
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
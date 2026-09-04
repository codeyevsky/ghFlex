package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
	"golang.org/x/term"
)

type args struct {
	opts map[string]string
}

func (a args) str(key, def string) string {
	if v, ok := a.opts[key]; ok && v != "" {
		return v
	}
	return def
}

func (a args) num(key string, def int) int {
	if v, ok := a.opts[key]; ok {
		n := 0
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

func (a args) bool(key string) bool { _, ok := a.opts[key]; return ok }

var shortcutRe = regexp.MustCompile(`^([^:]+):(followers|following|stargazers|watchers|stars)$`)
var repoRe = regexp.MustCompile(`^[^/]+/[^/]+$`)

func normalizeURL(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if m := shortcutRe.FindStringSubmatch(raw); m != nil {
		who, kind := m[1], m[2]
		if kind == "stargazers" || kind == "watchers" {
			return fmt.Sprintf("https://github.com/%s/%s", who, kind)
		}
		return fmt.Sprintf("https://github.com/%s?tab=%s", who, kind)
	}
	if repoRe.MatchString(raw) {
		return "https://github.com/" + raw + "/stargazers"
	}
	return "https://github.com/" + raw + "?tab=followers"
}

func showStats() {
	s := LoadState()
	fmt.Printf("  Followed:  %d\n", len(s.Followed))
	fmt.Printf("  Starred:   %d\n", len(s.Starred))
	fmt.Printf("  Skipped:   %d\n", len(s.Skipped))
	fmt.Printf("  Runs:      %d\n", len(s.Runs))
	from := 0
	if len(s.Runs) > 5 {
		from = len(s.Runs) - 5
	}
	for _, r := range s.Runs[from:] {
		sign := "+"
		if r.Mode == "unfollow" || r.Mode == "unstar" {
			sign = "-"
		}
		dry := ""
		if r.DryRun {
			dry = " [dry-run]"
		}
		fmt.Printf("    %s  %-8s %s%d  %d pages  (%s)%s  %s\n", r.At, r.Mode, sign, r.Count, r.Pages, r.Stopped, dry, r.URL)
	}
	printLast := func(title string, m map[string]Entry, width int) {
		if len(m) == 0 {
			return
		}
		type kv struct {
			name string
			at   string
		}
		var list []kv
		for k, v := range m {
			list = append(list, kv{k, v.At})
		}
		sort.Slice(list, func(i, j int) bool { return list[i].at < list[j].at })
		if len(list) > 10 {
			list = list[len(list)-10:]
		}
		fmt.Println("\n  " + title)
		for _, e := range list {
			fmt.Printf("    %-*s %s\n", width, e.name, e.at)
		}
	}
	printLast("Last 10 follows:", s.Followed, 24)
	printLast("Last 10 stars:", s.Starred, 32)
}

type reqStatus struct {
	Driver   bool
	Firefox  bool
	Chromium bool
}

func (r reqStatus) ok() bool { return r.Driver && r.Firefox && r.Chromium }

// checkRequirements probes the Playwright driver and browser binaries without
// launching anything visible.
func checkRequirements() reqStatus {
	st := reqStatus{}
	pw, err := playwright.Run()
	if err != nil {
		return st
	}
	defer func() { _ = pw.Stop() }()
	st.Driver = true
	if p := pw.Firefox.ExecutablePath(); p != "" {
		_, err := os.Stat(p)
		st.Firefox = err == nil
	}
	if p := pw.Chromium.ExecutablePath(); p != "" {
		_, err := os.Stat(p)
		st.Chromium = err == nil
	}
	return st
}

func printStatus(st reqStatus) {
	mark := func(ok bool) string {
		if ok {
			return tint(cGreen, "OK")
		}
		return tint(cRed, "missing")
	}
	fmt.Println("  Requirements:")
	fmt.Printf("    playwright driver : %s\n", mark(st.Driver))
	fmt.Printf("    firefox           : %s\n", mark(st.Firefox))
	fmt.Printf("    chromium          : %s\n", mark(st.Chromium))
}

func installMissing(st reqStatus) error {
	var browsers []string
	if !st.Firefox {
		browsers = append(browsers, "firefox")
	}
	if !st.Chromium {
		browsers = append(browsers, "chromium")
	}
	if st.Driver && len(browsers) == 0 {
		fmt.Println("  Everything is already installed.")
		return nil
	}
	if len(browsers) == 0 {
		browsers = []string{"firefox", "chromium"}
	}
	fmt.Printf("  Downloading: driver + %s (a few hundred MB, one time)...\n", strings.Join(browsers, ", "))
	err := playwright.Install(&playwright.RunOptions{Browsers: browsers})
	if err == nil {
		fmt.Println("  Done.")
	}
	return err
}

// runCommand runs one panel action against a fresh browser context.
func runCommand(cmd string, a args) error {
	browser := a.str("browser", "firefox")
	_, isMode := Modes[cmd]
	isAction := isMode || cmd == "startree"

	s, err := OpenBrowser(BrowserOpts{
		Browser:      browser,
		Headless:     isAction && a.bool("headless"),
		System:       a.bool("system-chromium"),
		UseMyProfile: a.bool("use-my-profile"),
	})
	if err != nil {
		return err
	}
	defer s.Close()

	if cmd == "login" {
		if who := IsLoggedIn(s); who != "" {
			fmt.Printf("  Already logged in as %s. You can close the window.\n", who)
			return nil
		}
		if _, err := s.Page.Goto("https://github.com/login", playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		}); err != nil {
			return err
		}
		fmt.Println("  Sign in to GitHub in the browser (including 2FA). This closes once login is detected...")
		who, err := WaitForLogin(s, 5*time.Minute)
		if err != nil {
			return err
		}
		fmt.Printf("  Logged in as %s\n", who)
		return nil
	}

	if cmd == "whoami" {
		if who := IsLoggedIn(s); who != "" {
			fmt.Printf("  Logged in as %s\n", who)
		} else {
			fmt.Println("  Not logged in. Use the login option first.")
		}
		return nil
	}

	who := IsLoggedIn(s)
	if who == "" {
		if a.bool("use-my-profile") {
			return fmt.Errorf("not logged in in your real profile. Open GitHub there and sign in once, or use the login option")
		}
		return fmt.Errorf("not logged in. Use the login option first")
	}

	if cmd == "startree" {
		root := a.str("user", "torvalds")
		if root == "me" || root == "" {
			root = who
		}
		dry := ""
		if a.bool("dry-run") {
			dry = " | DRY RUN"
		}
		fmt.Printf("  Account: %s | startree from %s | browser: %s%s\n", who, root, browser, dry)
		stats, err := RunStarTree(s.Page, TreeOptions{
			RootUser:     root,
			MaxDepth:     a.num("depth", 1),
			PagesPerUser: a.num("pages", 1),
			MaxActions:   a.num("max", 30),
			MinDelay:     time.Duration(a.num("min-delay", 4000)) * time.Millisecond,
			MaxDelay:     time.Duration(a.num("max-delay", 9000)) * time.Millisecond,
			PageDelay:    time.Duration(a.num("page-delay", 6000)) * time.Millisecond,
			DryRun:       a.bool("dry-run"),
		})
		if err != nil {
			return err
		}
		fmt.Printf("\n  %s %d starred, %d skipped, %d users visited - %s\n", tint(cGreen, "Done:"),
			len(stats.Done), stats.Skipped, stats.Pages, stats.Stopped)
		rateLimitNotice(stats.Stopped)
		return nil
	}

	// Resolve the "me" shortcut to the logged-in account.
	raw := a.opts["url"]
	if raw == "me" {
		if cmd == "star" || cmd == "unstar" {
			raw = who + ":stars"
		} else {
			raw = who + ":following"
		}
	} else if strings.HasPrefix(raw, "me:") {
		raw = who + raw[2:]
	}
	target := normalizeURL(raw)
	if target == "" {
		return fmt.Errorf("a target is required, e.g. me:following or torvalds:stars")
	}

	dry := ""
	if a.bool("dry-run") {
		dry = " | DRY RUN"
	}
	profile := ""
	if a.bool("use-my-profile") {
		profile = " (your profile)"
	}
	fmt.Printf("  Account: %s | %s | browser: %s%s%s\n", who, cmd, browser, profile, dry)

	stats, err := RunAction(s.Page, RunOptions{
		URL:        target,
		Mode:       cmd,
		MaxPages:   a.num("pages", 3),
		MaxActions: a.num("max", 30),
		MinDelay:   time.Duration(a.num("min-delay", 4000)) * time.Millisecond,
		MaxDelay:   time.Duration(a.num("max-delay", 9000)) * time.Millisecond,
		PageDelay:  time.Duration(a.num("page-delay", 6000)) * time.Millisecond,
		DryRun:     a.bool("dry-run"),
	})
	if err != nil {
		return err
	}
	fmt.Printf("\n  %s %d %s, %d skipped, %d pages - %s\n", tint(cGreen, "Done:"),
		len(stats.Done), Modes[cmd].Past, stats.Skipped, stats.Pages, stats.Stopped)
	rateLimitNotice(stats.Stopped)
	return nil
}

// rateLimitNotice prints a clear, calm warning when a run stopped because
// GitHub throttled it, instead of the process just ending abruptly.
func rateLimitNotice(stopped string) {
	if !strings.Contains(stopped, "rate limit") {
		return
	}
	fmt.Println()
	fmt.Println("  " + tint(cRed, "!! GitHub usage limit reached (rate limited)."))
	fmt.Println("  " + tint(cYellow, "   You've hit GitHub's action limit. Wait ~30 minutes"))
	fmt.Println("  " + tint(cYellow, "   before trying again to keep your account safe."))
	fmt.Println("  " + tint(cDim, "   Nothing crashed - your progress so far is saved."))
}

var menuItems = []menuItem{
	{"setup", "check / download requirements"},
	{"login", "save a GitHub session (opens a browser)"},
	{"follow", "follow everyone on a list page"},
	{"unfollow", "unfollow from a list page"},
	{"star", "star every repo on someone's stars page"},
	{"unstar", "unstar repos (usually your own stars)"},
	{"startree", "star tree: branch through repo owners' stars"},
	{"stats", "history and totals"},
	{"whoami", "which account is logged in"},
	{"quit", "exit"},
}

// Only follow/star ask for a target (they act on someone else's list);
// unfollow/unstar always work on your own list, so they don't appear here.
var targetPrompt = map[string]string{
	"follow": "who to follow  (e.g. username:followers, username:following)",
	"star":   "whose stars to star  (e.g. username:stars, owner/repo:stargazers)",
}

// Delay presets: {min, max, page} in milliseconds. "medium" is the default,
// "fast" trades safety for speed, "slow" is the most cautious.
var speedPreset = map[string][3]int{
	"slow":   {2000, 4000, 3000},
	"medium": {600, 1400, 1000},
	"fast":   {150, 450, 300},
}

// Order shown in the chooser and the index highlighted first (medium).
var speedOptions = []string{"slow", "medium", "fast"}

const speedDefaultIdx = 1

// max prompt label per mode, so it reads "max follows" / "max stars" etc.
var maxLabel = map[string]string{
	"follow":   "max follows",
	"unfollow": "max unfollows",
	"star":     "max stars",
	"unstar":   "max unstars",
}

func applySpeed(a *args, choice string) {
	p, ok := speedPreset[choice]
	if !ok {
		p = speedPreset["recommended"]
	}
	a.opts["min-delay"] = strconv.Itoa(p[0])
	a.opts["max-delay"] = strconv.Itoa(p[1])
	a.opts["page-delay"] = strconv.Itoa(p[2])
}

var digitsRe = regexp.MustCompile(`[^0-9]`)

func panel() {
	in := bufio.NewReader(os.Stdin)
	interactive := term.IsTerminal(int(os.Stdin.Fd()))
	ask := func(q, def string) string {
		if def != "" {
			fmt.Printf("  %s [%s]: ", q, def)
		} else {
			fmt.Printf("  %s: ", q)
		}
		line, err := in.ReadString('\n')
		if err != nil {
			return "q"
		}
		line = sanitizeInput(line)
		if line == "" {
			return def
		}
		return line
	}
	yes := func(q, def string) bool {
		return strings.HasPrefix(strings.ToLower(ask(q, def)), "y")
	}
	// askInt keeps only digits, so letters or symbols can't slip into a numeric
	// field; an empty result falls back to the default.
	askInt := func(q string, def int) string {
		v := digitsRe.ReplaceAllString(ask(q, strconv.Itoa(def)), "")
		if v == "" {
			return strconv.Itoa(def)
		}
		return v
	}
	// askTarget requires a value (follow/star have no "your own" default), so an
	// action never starts without a target.
	askTarget := func(cmd string) string {
		for {
			v := ask(targetPrompt[cmd], "")
			if v != "" {
				return v
			}
			fmt.Println("  " + tint(cRed, "a target is required, e.g. username:followers"))
		}
	}
	pause := func() {
		if interactive {
			waitEnter(in)
		}
	}
	// Requirements are only probed when the user picks "setup"; the header
	// just reflects the last check of this session.
	var st *reqStatus
	reqLine := func() string {
		if st == nil {
			return tint(cYellow, "not checked") + tint(cDim, " -- pick 'setup' to check / download")
		}
		if st.ok() {
			return tint(cGreen, "OK")
		}
		return tint(cRed, "MISSING") + tint(cDim, " -- pick 'setup' to download")
	}

	// The panel unrolls line by line the first time it appears; later redraws
	// are instant so returning from an action isn't slowed down.
	firstReveal := colorOn && interactive
	for {
		clearScreen(interactive)
		printBanner(firstReveal)
		revealLn("   requirements: "+reqLine(), firstReveal)
		revealLn("   data: "+tint(cDim, DataDir()), firstReveal)

		idx, ok := selectMenu(menuItems, in, interactive, firstReveal)
		firstReveal = false
		if !ok {
			fmt.Println()
			return
		}
		cmd := menuItems[idx].cmd
		if cmd == "quit" {
			fmt.Println()
			return
		}

		fmt.Println()
		flushLine("  ", fmt.Sprintf("==[ %s ]%s", cmd, strings.Repeat("=", 50-len(cmd))), cPurple)
		fmt.Println()

		switch cmd {
		case "setup":
			fmt.Println("  checking requirements...")
			cur := checkRequirements()
			st = &cur
			printStatus(cur)
			if !cur.ok() && yes("Download the missing pieces now? (Y/n)", "y") {
				if err := installMissing(cur); err != nil {
					fmt.Fprintf(os.Stderr, "  %s\n", tint(cRed, fmt.Sprintf("error: %v", err)))
				} else {
					cur = checkRequirements()
					st = &cur
					printStatus(cur)
				}
			}
			pause()
			continue
		case "stats":
			showStats()
			pause()
			continue
		}

		a := args{opts: map[string]string{}}
		// Every command that opens a browser asks which one, so whoami can check
		// either profile's session too.
		a.opts["browser"] = pick("browser", []string{"firefox", "chromium"}, 0, in, interactive)

		askSpeed := func() {
			hints := []string{"", "", "faster, but may trip GitHub's rate limit sooner"}
			sp := pickHint("speed", speedOptions, hints, speedDefaultIdx, in, interactive)
			applySpeed(&a, sp)
		}
		if _, isMode := Modes[cmd]; isMode {
			// follow/star act on someone else's list, so they need a target;
			// unfollow/unstar can only touch your own, so they skip the question.
			switch cmd {
			case "unfollow":
				a.opts["url"] = "me:following"
			case "unstar":
				a.opts["url"] = "me:stars"
			default:
				a.opts["url"] = askTarget(cmd)
			}
			a.opts["pages"] = askInt("pages to walk", 3)
			a.opts["max"] = askInt(maxLabel[cmd], 30)
			askSpeed()
			if pick("dry run", []string{"no", "yes"}, 0, in, interactive) == "yes" {
				a.opts["dry-run"] = ""
			}
		} else if cmd == "startree" {
			a.opts["user"] = ask("root user (whose stars to start from)", "torvalds")
			a.opts["depth"] = askInt("branch depth (0 = only the root's stars)", 1)
			a.opts["pages"] = askInt("pages per user", 1)
			a.opts["max"] = askInt("max stars total", 30)
			askSpeed()
			if pick("dry run", []string{"no", "yes"}, 0, in, interactive) == "yes" {
				a.opts["dry-run"] = ""
			}
		}

		if err := safeRun(cmd, a); err != nil {
			fmt.Fprintf(os.Stderr, "  %s\n", tint(cRed, fmt.Sprintf("error: %v", err)))
		}
		pause()
	}
}

// safeRun runs one command and turns any unexpected panic into an error, so a
// crash in the browser layer can never take the whole panel down.
func safeRun(cmd string, a args) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("unexpected error: %v", r)
		}
	}()
	return runCommand(cmd, a)
}

func main() {
	// githubFlex is fully interactive: no subcommands, everything happens in
	// the panel.
	if len(os.Args) > 1 {
		fmt.Println("githubFlex has no subcommands; everything is driven from the interactive panel.")
	}
	MigrateData()
	panel()
}

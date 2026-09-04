package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
	"github.com/mxschmitt/playwright-go"
)

var systemChromium = []string{
	"/usr/bin/chromium",
	"/usr/bin/chromium-browser",
	"/usr/bin/google-chrome-stable",
	"/usr/bin/brave",
}

type Session struct {
	pw   *playwright.Playwright
	ctx  playwright.BrowserContext
	Page playwright.Page
}

func (s *Session) Close() {
	if s.ctx != nil {
		_ = s.ctx.Close()
	}
	if s.pw != nil {
		_ = s.pw.Stop()
	}
}

func findSystemChromium() string {
	for _, p := range systemChromium {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Common profile locations, used by --use-my-profile (chromium only).
func realProfileDir() string {
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(home, ".config", "chromium"),
		filepath.Join(home, ".config", "google-chrome"),
		filepath.Join(home, ".config", "BraveSoftware", "Brave-Browser"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

type BrowserOpts struct {
	Browser      string // firefox | chromium
	Headless     bool
	System       bool // prefer /usr/bin/chromium over Playwright's build
	UseMyProfile bool // drive the real Chromium profile
}

// OpenBrowser launches a persistent context so the GitHub login is kept
// between runs. By default we use our own profile dir; with UseMyProfile the
// tool drives the real Chromium profile instead (close the browser first).
func OpenBrowser(o BrowserOpts) (*Session, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("playwright driver not ready (run: ghflex install): %w", err)
	}

	var engine playwright.BrowserType
	switch o.Browser {
	case "firefox":
		engine = pw.Firefox
	case "chromium", "chrome":
		engine = pw.Chromium
	default:
		_ = pw.Stop()
		return nil, fmt.Errorf("unknown browser: %s (use firefox or chromium)", o.Browser)
	}

	opts := playwright.BrowserTypeLaunchPersistentContextOptions{
		Headless: playwright.Bool(o.Headless),
		Viewport: &playwright.Size{Width: 1280, Height: 900},
	}
	if o.Browser != "firefox" {
		opts.Args = []string{"--disable-blink-features=AutomationControlled"}
	}

	exe := findSystemChromium()
	if o.System && o.Browser != "firefox" && exe != "" {
		opts.ExecutablePath = playwright.String(exe)
	}

	var dir string
	if o.UseMyProfile {
		if o.Browser == "firefox" {
			_ = pw.Stop()
			return nil, fmt.Errorf("--use-my-profile currently supports chromium only")
		}
		dir = realProfileDir()
		if dir == "" {
			_ = pw.Stop()
			return nil, fmt.Errorf("--use-my-profile: no real chromium profile found")
		}
		if exe != "" {
			opts.ExecutablePath = playwright.String(exe) // real profile pairs with the real browser build
		}
	} else {
		dir = ProfilePath(o.Browser)
	}

	ctx, err := engine.LaunchPersistentContext(dir, opts)
	if err != nil {
		_ = pw.Stop()
		if o.UseMyProfile && regexp.MustCompile(`(?i)ProcessSingleton|SingletonLock|Failed to create|profile appears`).MatchString(err.Error()) {
			return nil, fmt.Errorf("can't open your real %s profile, it's in use.\nClose every %s window (check: pgrep -a %s) and try again",
				o.Browser, o.Browser, o.Browser)
		}
		return nil, err
	}
	ctx.SetDefaultTimeout(30000)

	var page playwright.Page
	if pages := ctx.Pages(); len(pages) > 0 {
		page = pages[0]
	} else if page, err = ctx.NewPage(); err != nil {
		_ = ctx.Close()
		_ = pw.Stop()
		return nil, err
	}
	return &Session{pw: pw, ctx: ctx, Page: page}, nil
}

func loginFromCookies(ctx playwright.BrowserContext) string {
	cookies, err := ctx.Cookies("https://github.com")
	if err != nil {
		return ""
	}
	loggedIn, user := false, ""
	for _, c := range cookies {
		if c.Name == "logged_in" && c.Value == "yes" {
			loggedIn = true
		}
		if c.Name == "dotcom_user" {
			user = c.Value
		}
	}
	if !loggedIn {
		return ""
	}
	if user == "" {
		return "user"
	}
	return user
}

func IsLoggedIn(s *Session) string {
	if _, err := s.Page.Goto("https://github.com/", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		return ""
	}
	// New GitHub dashboard dropped meta[name=user-login]; cookies are the reliable signal.
	if who := loginFromCookies(s.ctx); who != "" {
		return who
	}
	loc := s.Page.Locator(`meta[name="user-login"]`)
	if n, err := loc.Count(); err == nil && n > 0 {
		if v, err := loc.GetAttribute("content"); err == nil && v != "" {
			return v
		}
	}
	return ""
}

func WaitForLogin(s *Session, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if who := loginFromCookies(s.ctx); who != "" {
			return who, nil
		}
		s.Page.WaitForTimeout(2000)
	}
	return "", fmt.Errorf("login timed out")
}

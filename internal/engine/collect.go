package engine

import (
	"fmt"
	"strings"
	"github.com/mxschmitt/playwright-go"
)

var rateLimitHints = []string{
	"secondary rate limit",
	"abuse detection",
	"have been rate limited",
	"whoa there",
	"too many requests",
}

type target struct {
	Name   string
	Action string
	Token  string
}

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
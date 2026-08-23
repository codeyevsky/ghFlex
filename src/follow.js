import { loadState, saveState, blocklist } from './state.js';

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const jitter = (min, max) => Math.round(min + Math.random() * Math.max(0, max - min));

const RATE_LIMIT_HINTS = [
  'secondary rate limit',
  'abuse detection',
  'have been rate limited',
  'Whoa there',
  'Too Many Requests',
];

// A follower/following row carries both a follow and an unfollow form; GitHub hides
// one with CSS. The visible one tells us the current state: a visible follow form
// means we are not following that user, a visible unfollow form means we are.
async function collectTargets(page, skip, mode) {
  const list = await page.evaluate((verb) => {
    const vis = (el) => el && el.offsetParent !== null && getComputedStyle(el).display !== 'none';
    const isHeader = (el) =>
      !!el.closest('header, [class*="ProfileHeader"], .h-card, .vcard, [itemtype*="Person"]');
    const out = [];
    for (const form of document.querySelectorAll(`form[action^="/users/${verb}?target="]`)) {
      if (!vis(form) || isHeader(form)) continue;
      const m = form.getAttribute('action').match(/target=([^&]+)/);
      if (!m) continue;
      const user = decodeURIComponent(m[1]);
      const token = form.querySelector('input[name="authenticity_token"]')?.value;
      if (!token) continue;
      out.push({ user, action: form.action, token });
    }
    return out;
  }, mode);
  const seen = new Set();
  return list.filter((t) => {
    const k = t.user.toLowerCase();
    if (skip.has(k) || seen.has(k)) return false;
    seen.add(k);
    return true;
  });
}

// Send the action the same way GitHub's own button does: a POST to the form action
// with the page's authenticity token, reusing the session cookies.
async function submit(page, target) {
  return page.evaluate(async ({ action, token }) => {
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
  }, target);
}

function isRateLimitResponse(res) {
  if (res.status === 429 || res.status === 403 || res.status === 422) return `HTTP ${res.status} (follow limit / throttled)`;
  const t = (res.text || '').toLowerCase();
  return RATE_LIMIT_HINTS.find((h) => t.includes(h.toLowerCase())) || null;
}

async function gotoNextPage(page) {
  // Clicking the Next link does not always navigate, so read its href and go there.
  const href = await page.evaluate(() => {
    const links = [...document.querySelectorAll('.pagination a, .paginate-container a, a[rel="next"]')];
    const next = links.find(
      (a) => /next/i.test(a.textContent || '') && a.getAttribute('aria-disabled') !== 'true'
    );
    return next ? next.href : null;
  });
  if (!href) return false;
  await page.goto(href, { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(800);
  return true;
}

// mode: 'follow' or 'unfollow'
export async function runAction(page, opts) {
  const {
    url,
    mode = 'follow',
    maxPages = 5,
    maxActions = 50,
    minDelay = 4000,
    maxDelay = 9000,
    pageDelay = 6000,
    dryRun = false,
    onLog = console.log,
  } = opts;

  const verb = mode === 'unfollow' ? 'unfollow' : 'follow';
  const past = verb === 'unfollow' ? 'unfollowed' : 'followed';
  const state = loadState();
  const blocked = blocklist();
  const skip = new Set([...blocked]);
  try {
    const seg = new URL(url).pathname.split('/').filter(Boolean);
    if (seg.length) skip.add(seg[0].toLowerCase()); // profile owner
  } catch {}

  const stats = { done: [], skipped: 0, pages: 0, stopped: null, mode };
  let consecutiveFail = 0;

  onLog(`→ ${verb}: ${url}`);
  await page.goto(url, { waitUntil: 'domcontentloaded' });

  for (let p = 1; p <= maxPages; p++) {
    stats.pages = p;
    onLog(`\n── page ${p} · ${page.url()}`);

    const targets = await collectTargets(page, skip, verb);
    if (targets.length === 0) onLog(`   (no users to ${verb} on this page)`);

    for (const t of targets) {
      if (stats.done.length >= maxActions) break;
      skip.add(t.user.toLowerCase());

      if (dryRun) {
        onLog(`   [dry-run] would ${verb} ${t.user}`);
        stats.done.push(t.user);
        continue;
      }

      const res = await submit(page, t);
      const rl = isRateLimitResponse(res);
      if (rl) {
        stats.stopped = `rate limited (${rl})`;
        onLog(`   ! stopping: ${stats.stopped}`);
        break;
      }

      if (res.ok) {
        consecutiveFail = 0;
        stats.done.push(t.user);
        if (verb === 'follow') {
          state.followed[t.user] = { at: new Date().toISOString(), from: url };
        } else {
          delete state.followed[t.user];
        }
        saveState(state);
        onLog(`   ${verb === 'follow' ? '✓' : '×'} ${t.user}  (${stats.done.length}/${maxActions})`);
      } else {
        stats.skipped++;
        state.skipped[t.user] = { at: new Date().toISOString(), reason: `HTTP ${res.status}` };
        saveState(state);
        onLog(`   ? ${t.user}: HTTP ${res.status} - skipped`);
        if (++consecutiveFail >= 5) {
          stats.stopped = `stopped: ${consecutiveFail} failures in a row (likely follow limit)`;
          onLog(`   ! ${stats.stopped}`);
          break;
        }
      }

      await sleep(jitter(minDelay, maxDelay));
    }

    if (stats.stopped) break;
    if (stats.done.length >= maxActions) { stats.stopped = 'reached --max'; break; }
    onLog(`   page done (${stats.done.length} total so far)`);

    if (p === maxPages) { stats.stopped = 'reached --pages'; break; }
    await sleep(pageDelay);
    if (!(await gotoNextPage(page))) { stats.stopped = 'no more pages'; break; }
  }

  state.runs.push({
    at: new Date().toISOString(),
    url,
    mode,
    [past]: stats.done.length,
    pages: stats.pages,
    stopped: stats.stopped,
    dryRun,
  });
  saveState(state);
  return stats;
}

// Back-compat wrapper: follow mode with the old return shape (stats.followed).
export async function runFollow(page, opts) {
  const stats = await runAction(page, { ...opts, mode: 'follow', maxActions: opts.maxFollows ?? opts.maxActions });
  return { followed: stats.done, skipped: stats.skipped, pages: stats.pages, stopped: stats.stopped };
}

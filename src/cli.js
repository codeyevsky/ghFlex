#!/usr/bin/env node
import { openBrowser, isLoggedIn, waitForLogin } from './browser.js';
import { runAction } from './follow.js';
import { loadState, DATA_DIR } from './state.js';

const HELP = `gh-autofollow - follow everyone on a GitHub list page, then move to the next page.

Usage:
  ghaf login    [--browser firefox|chromium]
  ghaf run      --url <github-url> [options]   follow everyone on the page(s)
  ghaf unfollow --url <github-url> [options]   unfollow everyone on the page(s)
  ghaf stats
  ghaf whoami   [--browser ...]

Options:
  --browser <firefox|chromium>  Default: firefox
  --url <url>                   Start page. Shortcuts: "user:followers",
                                "user:following", "owner/repo:stargazers".
                                Use "me" for yourself, e.g. "me:following".
  --pages <n>        How many pages to walk (default 3)
  --max <n>          Cap on follows/unfollows for the run (default 30)
  --min-delay <ms>   Min wait between actions (default 4000)
  --max-delay <ms>   Max wait between actions (default 9000)
  --page-delay <ms>  Wait between page changes (default 6000)
  --dry-run          Do nothing; just print who would be affected
  --headless         Hide the window (after login)
  --system-chromium  Use /usr/bin/chromium instead of Playwright's build
  --use-my-profile   Reuse your REAL browser profile (existing GitHub login).
                     Chromium only; close every browser window first.

State files: ${DATA_DIR}
  state.json     -> followed users / run history
  blocklist.txt  -> one username per line; these are skipped
`;

function parseArgs(argv) {
  const out = { _: [] };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a.startsWith('--')) {
      const key = a.slice(2);
      const next = argv[i + 1];
      if (next === undefined || next.startsWith('--')) out[key] = true;
      else { out[key] = next; i++; }
    } else out._.push(a);
  }
  return out;
}

function normalizeUrl(raw) {
  if (!raw) return null;
  if (/^https?:\/\//.test(raw)) return raw;
  const m = raw.match(/^([^:]+):(followers|following|stargazers|watchers)$/);
  if (m) {
    const [, who, kind] = m;
    if (kind === 'stargazers' || kind === 'watchers') return `https://github.com/${who}/${kind}`;
    return `https://github.com/${who}?tab=${kind}`;
  }
  if (/^[^/]+\/[^/]+$/.test(raw)) return `https://github.com/${raw}/stargazers`;
  return `https://github.com/${raw}?tab=followers`;
}

const num = (v, d) => (v === undefined ? d : Number(v));

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const cmd = args._[0] || 'help';
  const browser = args.browser === true ? 'firefox' : args.browser || 'firefox';
  const useMyProfile = !!args['use-my-profile'];

  if (cmd === 'help' || args.help) return console.log(HELP);

  if (cmd === 'stats') {
    const s = loadState();
    const f = Object.entries(s.followed);
    console.log(`Followed:  ${f.length}`);
    console.log(`Skipped:   ${Object.keys(s.skipped).length}`);
    console.log(`Runs:      ${s.runs.length}`);
    for (const r of s.runs.slice(-5)) { const n = r.mode === 'unfollow' ? `-${r.unfollowed ?? 0}` : `+${r.followed ?? 0}`; console.log(`  ${r.at}  ${n}  ${r.pages} pages  (${r.stopped})${r.dryRun ? ' [dry-run]' : ''}  ${r.url}`); }
    console.log('\nLast 10 follows:');
    for (const [u, m] of f.slice(-10)) console.log(`  ${u.padEnd(24)} ${m.at}`);
    return;
  }

  if (useMyProfile && browser === 'firefox') {
    console.error('--use-my-profile currently supports chromium only. Use --browser chromium.');
    process.exitCode = 1;
    return;
  }

  const { ctx, page } = await openBrowser({
    browser,
    headless: cmd === 'run' ? !!args.headless : false,
    system: !!args['system-chromium'],
    useMyProfile,
  });

  try {
    if (cmd === 'login') {
      const who = await isLoggedIn(page);
      if (who) { console.log(`Already logged in as ${who}. You can close the window.`); }
      else {
        await page.goto('https://github.com/login', { waitUntil: 'domcontentloaded' });
        console.log('Sign in to GitHub in the browser (including 2FA). This closes once login is detected...');
        const user = await waitForLogin(page);
        console.log(`Logged in as ${user}`);
      }
      return;
    }

    if (cmd === 'whoami') {
      const who = await isLoggedIn(page);
      console.log(who ? `Logged in as ${who}` : 'Not logged in. Run: ghaf login');
      return;
    }

    if (cmd !== 'run' && cmd !== 'unfollow') { console.log(HELP); process.exitCode = 1; return; }

    const who = await isLoggedIn(page);
    if (!who) {
      console.error(
        useMyProfile
          ? 'Not logged in in your real profile. Open GitHub there and sign in once, or run: ghaf login --browser ' + browser
          : 'Not logged in. Run first: ghaf login --browser ' + browser
      );
      process.exitCode = 1;
      return;
    }

    // Resolve the "me" shortcut to the logged-in account.
    let raw = args.url === true ? null : args.url;
    if (raw === 'me') raw = `${who}:following`;
    else if (typeof raw === 'string' && raw.startsWith('me:')) raw = who + raw.slice(2);
    const url = normalizeUrl(raw);
    if (!url) { console.error('--url is required. Example: --url me:following'); process.exitCode = 1; return; }

    const mode = cmd === 'unfollow' ? 'unfollow' : 'follow';
    console.log(`Account: ${who} · ${mode} · browser: ${browser}${useMyProfile ? ' (your profile)' : ''}${args['dry-run'] ? ' · DRY RUN' : ''}`);

    const stats = await runAction(page, {
      url,
      mode,
      maxPages: num(args.pages, 3),
      maxActions: num(args.max, 30),
      minDelay: num(args['min-delay'], 4000),
      maxDelay: num(args['max-delay'], 9000),
      pageDelay: num(args['page-delay'], 6000),
      dryRun: !!args['dry-run'],
    });

    const verb = mode === 'unfollow' ? 'unfollowed' : 'followed';
    console.log(`\nDone: ${stats.done.length} ${verb}, ${stats.skipped} skipped, ${stats.pages} pages - ${stats.stopped}`);
  } finally {
    await ctx.close().catch(() => {});
  }
}

main().catch((e) => { console.error(e.message || e); process.exit(1); });

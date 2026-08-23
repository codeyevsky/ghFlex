#!/usr/bin/env node
import { openBrowser, isLoggedIn, waitForLogin } from './browser.js';
import { runFollow } from './follow.js';
import { loadState, DATA_DIR } from './state.js';

const HELP = `gh-autofollow - follow everyone on a GitHub list page, then move to the next page.

Usage:
  ghaf login   [--browser firefox|chromium]
  ghaf run     --url <github-url> [options]
  ghaf stats
  ghaf whoami  [--browser ...]

Options:
  --browser <firefox|chromium>  Default: firefox
  --url <url>                   Start page. Shortcuts: "user:followers",
                                "user:following", "owner/repo:stargazers"
  --pages <n>        How many pages to walk (default 3)
  --max <n>          Total follow cap (default 30)
  --min-delay <ms>   Min wait between follows (default 4000)
  --max-delay <ms>   Max wait between follows (default 9000)
  --page-delay <ms>  Wait between page changes (default 6000)
  --dry-run          Click nothing; just print who would be followed
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
    for (const r of s.runs.slice(-5)) console.log(`  ${r.at}  +${r.followed}  ${r.pages} pages  (${r.stopped})${r.dryRun ? ' [dry-run]' : ''}  ${r.url}`);
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

    if (cmd !== 'run') { console.log(HELP); process.exitCode = 1; return; }

    const url = normalizeUrl(args.url === true ? null : args.url);
    if (!url) { console.error('--url is required. Example: --url torvalds:followers'); process.exitCode = 1; return; }

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
    console.log(`Account: ${who} · browser: ${browser}${useMyProfile ? ' (your profile)' : ''}${args['dry-run'] ? ' · DRY RUN' : ''}`);

    const stats = await runFollow(page, {
      url,
      maxPages: num(args.pages, 3),
      maxFollows: num(args.max, 30),
      minDelay: num(args['min-delay'], 4000),
      maxDelay: num(args['max-delay'], 9000),
      pageDelay: num(args['page-delay'], 6000),
      dryRun: !!args['dry-run'],
    });

    console.log(`\nDone: ${stats.followed.length} followed, ${stats.skipped} skipped, ${stats.pages} pages - ${stats.stopped}`);
  } finally {
    await ctx.close().catch(() => {});
  }
}

main().catch((e) => { console.error(e.message || e); process.exit(1); });

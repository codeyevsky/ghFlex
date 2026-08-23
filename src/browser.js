import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { chromium, firefox } from 'playwright';
import { profilePath } from './state.js';

const SYSTEM_CHROMIUM = ['/usr/bin/chromium', '/usr/bin/chromium-browser', '/usr/bin/google-chrome-stable', '/usr/bin/brave'];

export function resolveEngine(name) {
  if (name === 'firefox') return firefox;
  if (name === 'chromium' || name === 'chrome') return chromium;
  throw new Error(`unknown browser: ${name} (use firefox or chromium)`);
}

// Common profile locations, used by --use-my-profile.
export function realProfileDir(browser) {
  const home = os.homedir();
  if (browser === 'firefox') return null; // handled separately (profiles.ini), skip for now
  const candidates = [
    path.join(home, '.config', 'chromium'),
    path.join(home, '.config', 'google-chrome'),
    path.join(home, '.config', 'BraveSoftware', 'Brave-Browser'),
  ];
  return candidates.find((p) => fs.existsSync(p)) || null;
}

// Launch a persistent context so the GitHub login is kept between runs.
// By default we use our own profile dir. With useMyProfile the tool drives the
// real Chromium profile instead, reusing a login you already have; the normal
// browser has to be closed first because the profile is locked while it runs.
export async function openBrowser({ browser, headless, slowMo = 0, system = false, useMyProfile = false }) {
  const engine = resolveEngine(browser);
  const opts = {
    headless,
    slowMo,
    viewport: { width: 1280, height: 900 },
    args: browser === 'chromium' ? ['--disable-blink-features=AutomationControlled'] : [],
  };

  const exe = SYSTEM_CHROMIUM.find((p) => fs.existsSync(p));
  if (system && browser !== 'firefox' && exe) opts.executablePath = exe;

  let dir;
  if (useMyProfile) {
    const real = realProfileDir(browser);
    if (!real) throw new Error(`--use-my-profile: no real profile found for ${browser}`);
    if (browser !== 'firefox' && exe) opts.executablePath = exe; // real profile pairs with the real browser build
    dir = real;
  } else {
    dir = profilePath(browser);
  }

  let ctx;
  try {
    ctx = await engine.launchPersistentContext(dir, opts);
  } catch (e) {
    if (useMyProfile && /ProcessSingleton|SingletonLock|Failed to create|profile appears/i.test(e.message)) {
      throw new Error(
        `Can't open your real ${browser} profile, it's in use.\n` +
          `Close every ${browser} window (check: pgrep -a ${browser}) and try again.`
      );
    }
    throw e;
  }
  ctx.setDefaultTimeout(30000);
  const page = ctx.pages()[0] || (await ctx.newPage());
  return { ctx, page };
}

export async function loginFromCookies(ctx) {
  const cookies = await ctx.cookies('https://github.com');
  const loggedIn = cookies.find((c) => c.name === 'logged_in')?.value === 'yes';
  if (!loggedIn) return null;
  const user = cookies.find((c) => c.name === 'dotcom_user')?.value;
  return user || 'user';
}

export async function isLoggedIn(page) {
  await page.goto('https://github.com/', { waitUntil: 'domcontentloaded' });
  // New GitHub dashboard dropped meta[name=user-login]; cookies are the reliable signal.
  const fromCookie = await loginFromCookies(page.context());
  if (fromCookie) return fromCookie;
  const meta = await page.locator('meta[name="user-login"]').count();
  return meta > 0 ? (await page.getAttribute('meta[name="user-login"]', 'content')) || null : null;
}

export async function waitForLogin(page, timeoutMs = 5 * 60 * 1000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const who = await loginFromCookies(page.context());
    if (who) return who;
    await page.waitForTimeout(2000);
  }
  throw new Error('login timed out');
}

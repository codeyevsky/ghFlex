import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';

export const DATA_DIR =
  process.env.GH_AUTOFOLLOW_HOME ||
  path.join(process.env.XDG_DATA_HOME || path.join(os.homedir(), '.local', 'share'), 'gh-autofollow');

export const PROFILE_DIR = path.join(DATA_DIR, 'profiles');
const STATE_FILE = path.join(DATA_DIR, 'state.json');

const EMPTY = { followed: {}, skipped: {}, runs: [] };

export function loadState() {
  try {
    return { ...EMPTY, ...JSON.parse(fs.readFileSync(STATE_FILE, 'utf8')) };
  } catch {
    return structuredClone(EMPTY);
  }
}

export function saveState(state) {
  fs.mkdirSync(DATA_DIR, { recursive: true });
  fs.writeFileSync(STATE_FILE, JSON.stringify(state, null, 2));
}

export function profilePath(browser) {
  const p = path.join(PROFILE_DIR, browser);
  fs.mkdirSync(p, { recursive: true });
  return p;
}

export function blocklist() {
  const f = path.join(DATA_DIR, 'blocklist.txt');
  try {
    return new Set(
      fs.readFileSync(f, 'utf8').split('\n').map((l) => l.trim().toLowerCase()).filter((l) => l && !l.startsWith('#'))
    );
  } catch {
    return new Set();
  }
}

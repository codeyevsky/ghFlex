import { firefox } from 'playwright';
import { runFollow } from '../src/follow.js';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const dir = path.dirname(fileURLToPath(import.meta.url));
const b = await firefox.launch();
const p = await b.newPage();
const url = 'file://' + path.join(dir, 'fixtures/page1.html');

// dry-run: should pick only users whose FOLLOW form is visible (alice, carol),
// skip bob (already following -> unfollow visible).
const dry = await runFollow(p, { url, dryRun: true, maxPages: 1, maxFollows: 10, minDelay: 0, maxDelay: 0, pageDelay: 0 });
await b.close();

const expect = ['alice', 'carol'];
const pass = JSON.stringify(dry.followed) === JSON.stringify(expect);
console.log(`${pass ? 'PASS' : 'FAIL'} collect visible-follow-only: ${JSON.stringify(dry.followed)}`);
process.exit(pass ? 0 : 1);

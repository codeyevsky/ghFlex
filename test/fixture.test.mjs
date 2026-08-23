import { firefox } from 'playwright';
import { runAction } from '../src/follow.js';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const dir = path.dirname(fileURLToPath(import.meta.url));
const url = 'file://' + path.join(dir, 'fixtures/page1.html');
const b = await firefox.launch();
const p = await b.newPage();

// follow mode picks the rows whose follow form is visible (alice, carol);
// bob is skipped because its unfollow form is the visible one.
const f = await runAction(p, { url, mode: 'follow', dryRun: true, maxPages: 1, maxActions: 10, minDelay: 0, maxDelay: 0, pageDelay: 0 });
// unfollow mode picks the rows whose unfollow form is visible (bob).
const u = await runAction(p, { url, mode: 'unfollow', dryRun: true, maxPages: 1, maxActions: 10, minDelay: 0, maxDelay: 0, pageDelay: 0 });
await b.close();

const check = (name, got, want) => {
  const ok = JSON.stringify(got) === JSON.stringify(want);
  console.log(`${ok ? 'PASS' : 'FAIL'} ${name}: ${JSON.stringify(got)}`);
  return ok;
};
const ok = check('follow visible-only', f.done, ['alice', 'carol']) & check('unfollow visible-only', u.done, ['bob']);
process.exit(ok ? 0 : 1);

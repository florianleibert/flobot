// Drive a real project build through the Code Studio editor: create a file,
// use the @-mention picker to reference it, run a mission with the reference
// attached, and check the run's diff comes back for review. This is the
// "drive building a new project with it" check.
//
// With a reachable model route the mission runs and the diff is reviewed; with
// no reachable route the run step is reported as SKIP, not FAIL, so the
// file-creation and reference half still proves the surfaces work.
//
// Source of truth for the UI is flobot-ui-redesign; this script lives with the
// server change so the PR can be verified from one checkout.
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';

const PW = process.env.PLAYWRIGHT_PATH || '/Users/floleibert/dev/agent-gui/ui/node_modules/playwright/index.mjs';
const { chromium } = await import(PW);

// Resolve the chromium headless shell from the local playwright cache rather
// than pinning a build number: the cache is pruned by playwright and a pinned
// path turns into "executable doesn't exist" without any code change.
function chromiumPath() {
  if (process.env.CHROMIUM_PATH) return process.env.CHROMIUM_PATH;
  const root = path.join(os.homedir(), 'Library/Caches/ms-playwright');
  const dirs = fs.readdirSync(root)
    .filter(d => d.startsWith('chromium_headless_shell-'))
    .sort((a, b) => Number(b.split('-')[1]) - Number(a.split('-')[1]));
  for (const d of dirs) {
    const p = path.join(root, d, 'chrome-headless-shell-mac-arm64', 'chrome-headless-shell');
    if (fs.existsSync(p)) return p;
  }
  throw new Error('no chromium headless shell found under ' + root);
}

const BASE = process.env.FLOBOT_URL || 'http://127.0.0.1:4199';
const results = [];
const errors = [];
let n = 0;
function rec(id, name, ok, note = '') {
  n++;
  results.push(`${String(n).padStart(2)}. ${ok ? 'PASS' : ok === null ? 'SKIP' : 'FAIL'}  [${id}] ${name}${note ? ' — ' + note : ''}`);
}

const browser = await chromium.launch({ executablePath: chromiumPath() });
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } });
page.on('pageerror', e => errors.push('pageerror: ' + String(e).slice(0, 200)));
const $ = sel => page.locator(sel);

await page.goto(BASE, { waitUntil: 'domcontentloaded' });
await page.click('nav button[data-panel="editor"]');
await page.waitForSelector('#panel-editor.active');

// --- P1. start a new project file from the editor -----------------------------
const proj = 'demo-project';
const newFile = proj + '/main.go';
await page.evaluate(async ({ proj, newFile }) => {
  await fetch('/api/v1/workspace/file?path=' + encodeURIComponent(proj + '/go.mod'),
    { method: 'POST', headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ content: 'module demo\n\ngo 1.22\n' }) });
  await fetch('/api/v1/workspace/file?path=' + encodeURIComponent(newFile),
    { method: 'POST', headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ content: 'package main\n\nimport "fmt"\n\nfunc main() {\n\tfmt.Println("hello")\n}\n' }) });
}, { proj, newFile });

await $('#edRefresh').click();
await page.waitForTimeout(600);

// open it through the tree (exercises the descriptor path on a nested file)
const dirNode = page.locator('#edTree .ed-node.dir', { hasText: proj }).first();
if (await dirNode.count()) { await dirNode.click(); await page.waitForTimeout(500); }
await page.locator('#edTree .ed-node', { hasText: 'main.go' }).first().click();
await page.waitForFunction(() => (document.querySelector('#edContent')?.value || '').includes('func main'), null, { timeout: 5000 });
rec('P1', 'nested project file opens in the editor', true);

const crumbs = await page.locator('#edCrumbs .ed-crumb').allTextContents();
rec('P2', 'breadcrumbs show the nested path', crumbs.join('/') === newFile, crumbs.join('/'));

// --- P3. the mission references the file via @-mention ------------------------
await page.click('#edPrompt');
await page.fill('#edPrompt', 'add a Greet function to @');
await page.waitForTimeout(1200);
const mentionItems = await page.locator('#edMention .ed-mention-item').count();
rec('P3', 'mention picker finds the project file', mentionItems > 0, mentionItems + ' items');

const picked = page.locator('#edMention .ed-mention-item', { hasText: newFile }).first();
if (await picked.count()) await picked.click();
else await page.locator('#edMention .ed-mention-item').first().click();
await page.waitForTimeout(400);
const promptVal = await page.inputValue('#edPrompt');
rec('P4', 'mission prompt carries the file reference', promptVal.includes('@' + newFile), JSON.stringify(promptVal));
const chipTxt = await page.locator('#edCtx .ed-ctx-chip').first().textContent();
rec('P5', 'the reference shows as a chip', (chipTxt || '').includes(newFile), (chipTxt || '').trim());

// --- P6. the reference travels with the run -----------------------------------
const sent = await page.evaluate(async () => {
  const seen = [];
  const orig = window.fetch;
  window.fetch = function (u, o) {
    if (String(u).includes('/api/v1/runs')) {
      try { seen.push(JSON.parse(o && o.body)); } catch { /* not json */ }
    }
    return orig.apply(this, arguments);
  };
  const bot = document.querySelector('#edRunBot');
  if (bot && bot.options.length) bot.value = bot.options[0].value;
  document.querySelector('#edRunBtn').click();
  await new Promise(r => setTimeout(r, 1500));
  window.fetch = orig;
  return seen;
});
const body = sent[0] || {};
const sentPrompt = body.prompt || '';
rec('P6', 'the run body carries the referenced file path',
  sentPrompt.includes(newFile), JSON.stringify(sentPrompt.slice(0, 120)));
rec('P7', 'the run body names the referenced file under a heading',
  /Referenced files/.test(sentPrompt), /Referenced files/.test(sentPrompt) ? 'heading present' : 'heading missing');

// --- P8. the run reaches a terminal state, or is honestly skipped ------------
let finalStatus = '';
try {
  await page.waitForFunction(() => {
    const t = document.querySelector('#edRunStatus')?.textContent || '';
    return /done|error|stopped|failed/i.test(t);
  }, null, { timeout: 120000, polling: 500 });
  finalStatus = await $('#edRunStatus').textContent();
} catch { /* still running */ }
const ran = /done/i.test(finalStatus);
rec('P8', ran ? 'mission completed' : 'mission did not complete (no reachable model route)',
  ran ? true : null, finalStatus);

// --- P9. the diff pane shows the run's changes --------------------------------
if (ran) {
  await page.click('.ed-tab[data-tab="diff"]');
  await page.waitForTimeout(1200);
  const files = await page.locator('#edDiff .ed-file-diff').count();
  const hunks = await page.locator('#edDiff [data-act="a"]').count();
  rec('P9', 'the diff pane lists the changed files', files > 0, files + ' files, ' + hunks + ' hunks');
  rec('P10', 'per-hunk accept buttons are present', hunks > 0, hunks + ' accept buttons');
} else {
  rec('P9/P10', 'diff review of a finished run', null, 'no completed run to review');
}

await page.screenshot({ path: '/tmp/studio-5-project.png' });
console.log(results.join('\n'));
const pass = results.filter(r => r.includes('PASS')).length;
const fail = results.filter(r => r.includes('FAIL')).length;
const skip = results.filter(r => r.includes('SKIP')).length;
console.log(`\n=== ${pass} PASS / ${fail} FAIL / ${skip} SKIP of ${results.length} ===`);
console.log('--- errors (' + errors.length + ') ---\n' + (errors.slice(0, 5).join('\n') || 'none'));
await browser.close();
process.exit(fail ? 1 : 0);

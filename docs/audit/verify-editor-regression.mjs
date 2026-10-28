// Regression check for the interactions the descriptor refactor touched, plus
// a close-up of the @-mention popup. Runs against the real server.
const PW = process.env.PLAYWRIGHT_PATH || '/Users/floleibert/dev/agent-gui/ui/node_modules/playwright/index.mjs';
const { chromium } = await import(PW);
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';

const BASE = process.env.FLOBOT_URL || 'http://127.0.0.1:4199';
const results = [];
const errors = [];
let n = 0;
function rec(id, name, ok, note = '') {
  n++;
  results.push(`${String(n).padStart(2)}. ${ok ? 'PASS' : ok === null ? 'SKIP' : 'FAIL'}  [${id}] ${name}${note ? ' — ' + note : ''}`);
}

// Resolve the chromium headless shell from the local playwright cache rather
// than pinning a build number: the cache is pruned by playwright, and a pinned
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

const browser = await chromium.launch({ executablePath: chromiumPath() });
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } });
page.on('pageerror', e => errors.push('pageerror: ' + String(e).slice(0, 200)));
const $ = sel => page.locator(sel);

await page.goto(BASE, { waitUntil: 'domcontentloaded' });
await page.click('nav button[data-panel="editor"]');
await page.waitForSelector('#panel-editor.active');

// --- R1. open a missing file offers create, and creates it --------------------
const newPath = 'created-by-regression-' + Date.now() + '.js';
await page.evaluate(p => fetch('/api/v1/workspace/file?path=' + encodeURIComponent(p), { method: 'DELETE' }), newPath);
await $('#edPath').fill(newPath);
page.once('dialog', d => d.accept());
await page.evaluate(() => document.querySelector('#edOpen').click());
await page.waitForTimeout(1000);
const created = await page.evaluate(p => fetch('/api/v1/workspace/file?path=' + encodeURIComponent(p)).then(r => r.status), newPath);
rec('R1', 'Open missing file → confirm → creates it (path API unchanged)', created === 200, 'HTTP ' + created);
const bufPath = await page.inputValue('#edPath');
rec('R2', 'created file is now the open path', bufPath === newPath, bufPath);

// --- R3. saving an open file writes through the handle ------------------------
await page.fill('#edContent', 'edited while open');
await page.evaluate(() => document.querySelector('#edSave').click());
await page.waitForTimeout(1000);
const openFileContent = await page.evaluate(p => fetch('/api/v1/workspace/file?path=' + encodeURIComponent(p)).then(r => r.json()).then(j => j.content), newPath);
rec('R3', 'save writes to the open file', openFileContent === 'edited while open', JSON.stringify(openFileContent).slice(0, 50));

// --- R4. save-as when nothing is open -----------------------------------------
// A reload clears the open buffer (it is not persisted), which is the
// "nothing open, buffer has content" path the editor handles with a create.
await page.reload({ waitUntil: 'domcontentloaded' });
await page.click('nav button[data-panel="editor"]');
await page.waitForSelector('#panel-editor.active');
const saveAsPath = 'saved-as-' + Date.now() + '.txt';
await page.fill('#edContent', 'saved as new content');
await page.fill('#edPath', saveAsPath);
await page.evaluate(() => document.querySelector('#edSave').click());
await page.waitForTimeout(1200);
const savedAs = await page.evaluate(p => fetch('/api/v1/workspace/file?path=' + encodeURIComponent(p)).then(r => r.json()).then(j => j.content), saveAsPath);
rec('R4', 'save-as writes a new file', savedAs === 'saved as new content', JSON.stringify(savedAs).slice(0, 50));

// --- R5. quick-open Enter opens a file -----------------------------------------
// Seed the file the quick-open will pick, so the check does not depend on
// what the workspace happens to contain.
const QUICK_FILE = 'audit-quick-open.txt';
await page.evaluate(p => fetch('/api/v1/workspace/file?path=' + encodeURIComponent(p), {
  method: 'POST', headers: { 'content-type': 'application/json' },
  body: JSON.stringify({ content: 'quick open fixture\n' }),
}), QUICK_FILE);
await $('#edRefresh').click();
await page.waitForTimeout(600);
await page.keyboard.press('Control+p');
await page.waitForTimeout(600);
await $('#edQuickInput').fill('audit-quick-open');
await page.waitForTimeout(700);
await page.keyboard.press('Enter');
await page.waitForTimeout(1200);
const qOpen = await page.inputValue('#edPath');
rec('R5', 'quick-open Enter still opens a file', qOpen.includes(QUICK_FILE), qOpen);
const qClosed = await page.locator('#edQuick.open').count();
rec('R6', 'quick-open overlay closed after pick', qClosed === 0);

// --- R7. the mention popup sits under the prompt ------------------------------
await page.click('#edPrompt');
await page.fill('#edPrompt', 'refactor @');
await page.waitForTimeout(900);
const geo = await page.evaluate(() => {
  const p = document.querySelector('#edMention');
  const i = document.querySelector('#edPrompt');
  if (!p || p.hidden) return null;
  const pr = p.getBoundingClientRect(), ir = i.getBoundingClientRect();
  return { below: pr.top >= ir.bottom - 2, aligns: Math.abs(pr.left - ir.left) < 3, items: p.querySelectorAll('.ed-mention-item').length };
});
rec('R7', 'mention popup renders below the prompt, aligned to it', !!geo && geo.below && geo.aligns, JSON.stringify(geo));

if (geo) {
  const clip = await page.evaluate(() => {
    const p = document.querySelector('#edMention').getBoundingClientRect();
    return { x: p.x - 12, y: p.y - 46, width: p.width + 24, height: p.height + 58 };
  });
  await page.screenshot({ path: '/tmp/studio-3-mention-zoom.png', clip });
}

console.log(results.join('\n'));
const pass = results.filter(r => r.includes('PASS')).length;
const fail = results.filter(r => r.includes('FAIL')).length;
console.log(`\n=== ${pass} PASS / ${fail} FAIL of ${results.length} ===`);
console.log('--- errors (' + errors.length + ') ---\n' + (errors.slice(0, 5).join('\n') || 'none'));
await browser.close();
process.exit(fail ? 1 : 0);

// Focused Playwright check for the Code Studio context surfaces added in this
// change: breadcrumbs, the symbol outline, and the @-mention picker.
// Runs against the real server (no mocks).
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
page.on('console', m => { if (m.type() === 'error') errors.push('console: ' + m.text().slice(0, 200)); });
page.on('pageerror', e => errors.push('pageerror: ' + String(e).slice(0, 200)));

const $ = sel => page.locator(sel);

await page.goto(BASE, { waitUntil: 'domcontentloaded' });
await page.waitForSelector('nav button[data-panel="editor"]', { timeout: 15000 });
await page.click('nav button[data-panel="editor"]');
await page.waitForSelector('#panel-editor.active');

// --- A. open a file through the tree (descriptor path) ------------------------
// Seed a known fixture: an audit that depends on whatever the workspace
// happens to contain is not an audit.
const FIXTURE = 'audit-fixture.go';
const FIXTURE_SRC = [
  'package main',
  '',
  'import "fmt"',
  '',
  '// Greet says hello.',
  'func Greet(name string) string {',
  '\treturn fmt.Sprintf("hello %s", name)',
  '}',
  '',
  'type Server struct {',
  '\tPort int',
  '}',
  '',
  'func (s *Server) Start() error {',
  '\tfmt.Println("started")',
  '\treturn nil',
  '}',
  '',
].join('\n');
await page.evaluate(async ({ path, content }) => {
  const url = '/api/v1/workspace/file?path=' + encodeURIComponent(path);
  const cur = await fetch(url).then(r => r.ok ? r.json() : null);
  if (cur && cur.exists) {
    await fetch(url, { method: 'PUT', headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ content, baseSha256: cur.sha256 }) });
  } else {
    await fetch(url, { method: 'POST', headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ content }) });
  }
}, { path: FIXTURE, content: FIXTURE_SRC });

await $('#edRefresh').click();
await page.waitForTimeout(600);
await page.locator('#edTree .ed-node', { hasText: FIXTURE }).first().click();
await page.waitForFunction(() => (document.querySelector('#edContent')?.value || '').includes('func Greet'), null, { timeout: 5000 });
rec('A1', 'tree click opens file through the descriptor API', true);

const descCount = await page.evaluate(() =>
  fetch('/api/v1/workspace/descriptors').then(r => r.json()).then(j => (j.descriptors || []).length));
rec('A2', 'server holds an open descriptor for the file', descCount >= 1, descCount + ' descriptors');

// --- B. breadcrumbs -----------------------------------------------------------
const crumbs = await page.locator('#edCrumbs .ed-crumb').allTextContents();
rec('B1', 'breadcrumbs render the file path', crumbs.join('/') === FIXTURE, JSON.stringify(crumbs));
const crumbSep = await page.locator('#edCrumbs .ed-crumb-sep').count();
rec('B2', 'breadcrumb separator present', crumbSep === 0, 'single-segment path');

// --- C. symbol outline --------------------------------------------------------
const outlineItems = await page.locator('#edOutline .ed-outline-item').count();
const outlineNames = await page.locator('#edOutline .ed-outline-name').allTextContents();
rec('C1', 'outline lists Go symbols', outlineItems >= 3, outlineItems + ' symbols: ' + outlineNames.join(','));
rec('C2', 'outline finds the func declaration', outlineNames.includes('Greet'), outlineNames.join(','));
rec('C3', 'outline finds the type declaration', outlineNames.includes('Server'), outlineNames.join(','));

// clicking an outline entry moves the caret to that line
await page.locator('#edOutline .ed-outline-item', { hasText: 'Greet' }).first().click();
await page.waitForTimeout(250);
const sel = await page.evaluate(() => {
  const ta = document.querySelector('#edContent');
  return ta.value.slice(ta.selectionStart, ta.selectionEnd);
});
rec('C4', 'clicking an outline symbol selects its line', sel.includes('func Greet'), JSON.stringify(sel.slice(0, 40)));

// --- D. @-mention picker ------------------------------------------------------
await page.click('#edPrompt');
await page.fill('#edPrompt', 'look at @aud');
await page.waitForTimeout(900);
const mentionOpen = await page.locator('#edMention:not([hidden])').count();
rec('D1', '@ opens the mention picker', mentionOpen === 1);
const mentionItems = await page.locator('#edMention .ed-mention-item').count();
rec('D2', 'mention picker lists matching files', mentionItems > 0, mentionItems + ' items');

await page.locator('#edMention .ed-mention-item').first().click();
await page.waitForTimeout(300);
const promptVal = await page.inputValue('#edPrompt');
rec('D3', 'picking a mention inserts the path', promptVal.includes('@' + FIXTURE), JSON.stringify(promptVal));
const chips = await page.locator('#edCtx .ed-ctx-chip').count();
rec('D4', 'referenced file shows as a chip', chips === 1, chips + ' chips');
const ctxVisible = await page.locator('#edCtx').isVisible();
rec('D5', 'context chip row is visible', ctxVisible);

// removing the chip clears the reference
await page.locator('#edCtx .ed-ctx-x').first().click();
await page.waitForTimeout(200);
const chipsAfter = await page.locator('#edCtx .ed-ctx-chip').count();
rec('D6', 'removing the chip drops the reference', chipsAfter === 0, chipsAfter + ' chips');

// --- E. save goes through the descriptor ---------------------------------------
await page.locator('#edTree .ed-node', { hasText: FIXTURE }).first().click();
await page.waitForTimeout(400);
await page.fill('#edContent', 'package main\n\n// Greet says hello.\nfunc Greet(name string) string { return name }\n');
await page.waitForTimeout(150);
const dirty = await page.locator('#edDirty').textContent();
rec('E1', 'editing marks the buffer dirty', /unsaved/i.test(dirty), dirty);
await page.keyboard.press('Control+s');
await page.waitForFunction(() => !document.querySelector('#edDirty')?.textContent, null, { timeout: 5000 });
rec('E2', 'save through the descriptor clears dirty', true);
const onDisk = await page.evaluate(p => fetch('/api/v1/workspace/file?path=' + encodeURIComponent(p)).then(r => r.json()).then(j => j.content), FIXTURE);
rec('E3', 'save persisted to disk', onDisk.includes('return name'), JSON.stringify(onDisk).slice(0, 60));

// --- F. external change is refused --------------------------------------------
// A run (or another editor) rewrites the file under the open handle.
await page.evaluate(p => fetch('/api/v1/workspace/file?path=' + encodeURIComponent(p), {
  method: 'PUT', headers: { 'content-type': 'application/json' },
  body: JSON.stringify({ content: 'package main\n// external edit\n' }),
}).then(async () => {
  const cur = await fetch('/api/v1/workspace/file?path=' + encodeURIComponent(p)).then(x => x.json());
  return fetch('/api/v1/workspace/file?path=' + encodeURIComponent(p), {
    method: 'PUT', headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ content: 'package main\n// external edit\n', baseSha256: cur.sha256 }),
  });
}), FIXTURE);
await page.waitForTimeout(200);
await page.fill('#edContent', 'package main\n// mine\n');
await page.keyboard.press('Control+s');
await page.waitForTimeout(1200);
const toastTxt = await page.evaluate(() => {
  const t = [...document.querySelectorAll('.toast')].map(x => x.textContent);
  return t.join(' | ');
});
rec('F1', 'save refused when the file changed on disk', /changed on disk/i.test(toastTxt), toastTxt.slice(0, 90));
const afterConflict = await page.evaluate(p =>
  fetch('/api/v1/workspace/file?path=' + encodeURIComponent(p)).then(r => r.json()).then(j => j.content), FIXTURE);
rec('F2', 'the external edit is not clobbered', afterConflict.includes('external edit'), JSON.stringify(afterConflict).slice(0, 60));

await page.screenshot({ path: '/tmp/flobot-studio-context.png', fullPage: false });

console.log(results.join('\n'));
const pass = results.filter(r => r.includes('PASS')).length;
const fail = results.filter(r => r.includes('FAIL')).length;
console.log(`\n=== ${pass} PASS / ${fail} FAIL of ${results.length} ===`);
console.log('\n--- errors seen (' + errors.length + ') ---');
console.log(errors.slice(0, 10).join('\n') || 'none');
await browser.close();
process.exit(fail ? 1 : 0);

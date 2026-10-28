// Codex desktop parity audit: exercises the supervision surfaces against the
// real server — threads with worktrees, the review queue, the live diff and
// the workspace symbol palette.
//
// No model route is required: every check here is about wiring, not about
// an agent finishing work.
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
await page.waitForSelector('#sv-card', { timeout: 10000 });

// --- P1. the supervision card renders ---------------------------------------
{
  const head = await $('#sv-card .sv-head').innerText();
  rec('P1', 'supervision card renders with its sections', /Supervision/i.test(head), head.trim());
}

// --- P2. a symbol fixture exists for the palette ----------------------------
const fixtureName = 'CodexParityFixture';
await page.evaluate(async (name) => {
  await fetch('/api/v1/workspace/file?path=' + encodeURIComponent(name + '.go'), { method: 'DELETE' });
  await fetch('/api/v1/workspace/file?path=' + encodeURIComponent(name + '.go'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content: 'package main\n\nfunc ' + name + '() string { return "x" }\n' }),
  });
}, fixtureName);
await page.waitForTimeout(300);

// --- P3. the symbol palette opens on Cmd-Shift-O ----------------------------
{
  await page.keyboard.press('Meta+Shift+O');
  const shown = await page.evaluate(() => !document.getElementById('sv-pal')?.hidden);
  rec('P3', 'symbol palette opens on Cmd-Shift-O', shown === true);
}

// --- P4. the palette finds the symbol ---------------------------------------
{
  await page.fill('#sv-pal-input', fixtureName);
  await page.waitForFunction(
    (name) => document.querySelector('#sv-pal-list')?.textContent?.includes(name),
    fixtureName, { timeout: 6000 }).catch(() => {});
  const text = await $('#sv-pal-list').innerText();
  rec('P4', 'palette lists a workspace-wide symbol match', text.includes(fixtureName),
    text.replace(/\s+/g, ' ').trim().slice(0, 80));
}

// --- P5/P6. picking the symbol opens the file at the right line -------------
{
  await page.click('#sv-pal-list .sv-pal-item');
  await page.waitForTimeout(700);
  const openPath = await page.evaluate(() => document.getElementById('edPath')?.value || '');
  const caret = await page.evaluate(() => {
    const ta = document.getElementById('edContent');
    if (!ta) return -1;
    return ta.value.slice(0, ta.selectionStart).split('\n').length;
  });
  rec('P5', 'picking a symbol opens the file', openPath === fixtureName + '.go', 'open=' + openPath);
  rec('P6', 'the caret lands on the symbol line', caret === 3, 'line=' + caret);
}

// --- P7-P9. a thread appears in the card, with a remove control -------------
{
  const created = await page.evaluate(async () => {
    const r = await fetch('/api/v1/threads', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ bot_id: 'bot-glm', goal: 'audit thread', no_start: true }),
    });
    return r.ok ? await r.json() : null;
  });
  if (!created?.thread?.id) {
    rec('P7', 'thread created through the API', false, 'no thread in the response');
    rec('P8', 'the thread appears in the supervision card', false);
    rec('P9', 'thread row offers a remove control', false);
  } else {
    rec('P7', 'thread created through the API', true, created.thread.id);
    await page.evaluate(() => window.supervision?.refresh?.());
    await page.waitForTimeout(700);
    const shown = await page.evaluate(() => {
      const body = document.querySelector('#sv-card .sv-body');
      return body ? body.textContent.includes('audit thread') : false;
    });
    rec('P8', 'the thread appears in the supervision card', shown === true);
    const hasKill = await page.evaluate(() => {
      const body = document.querySelector('#sv-card .sv-body');
      return !!body?.querySelector('.sv-act[title*="worktree"]');
    });
    rec('P9', 'thread row offers a remove control', hasKill === true);
    // Clean up so the audit leaves no worktree behind.
    await page.evaluate((id) => fetch('/api/v1/threads/' + encodeURIComponent(id), { method: 'DELETE' }),
      created.thread.id);
  }
}

// --- P10-P12. the routine card shows a next fire time and can be edited -----
{
  await page.click('nav button[data-panel="routines"]');
  await page.waitForSelector('#panel-routines.active');
  const created = await page.evaluate(async () => {
    const sk = await (await fetch('/api/v1/skills', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'audit-skill', body: 'audit' }),
    })).json();
    const rt = await (await fetch('/api/v1/routines', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ bot_id: 'bot-glm', skill_id: sk.id, trigger: 'every 30m' }),
    })).json();
    return rt;
  });
  // The roster refreshes on explicit actions, not on a timer, so a routine
  // created through the API only reaches the panel after a reload. (That
  // staleness is pre-existing app behaviour, not something this epic
  // introduced.)
  await page.reload({ waitUntil: 'domcontentloaded' });
  await page.waitForSelector('#sv-card', { timeout: 10000 });
  await page.click('nav button[data-panel="routines"]');
  await page.waitForSelector('#panel-routines.active');
  await page.waitForTimeout(900);
  const cardText = await page.evaluate(() => {
    const li = document.querySelector('#routines li');
    return li ? li.textContent : '';
  });
  rec('P10', 'a routine card shows its next fire time', /next\s+\d{4}-\d{2}-\d{2}T/.test(cardText),
    cardText.replace(/\s+/g, ' ').trim().slice(0, 90));
  rec('P11', 'a routine card offers an in-place trigger edit',
    await page.evaluate(() => !!document.querySelector('#routines [data-rtn-edit-btn]')) === true);

  if (created?.id) {
    const changed = await page.evaluate(async (id) => {
      const r = await fetch('/api/v1/routines/' + encodeURIComponent(id), {
        method: 'PATCH', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ trigger: 'every 45m' }),
      });
      return r.ok ? await r.json() : null;
    }, created.id);
    rec('P12', 'a routine trigger can be patched in place',
      changed?.trigger === 'every 45m', 'trigger=' + (changed?.trigger || 'none'));
  } else {
    rec('P12', 'a routine trigger can be patched in place', false, 'no routine id');
  }
}

// --- P13. the live diff endpoint has the same shape as the settled one ------
{
  const shape = await page.evaluate(async () => {
    const live = await fetch('/api/v1/runs/run-missing/diff/live');
    const settled = await fetch('/api/v1/runs/run-missing/diff');
    return { live: live.status, settled: settled.status };
  });
  // Both must fail the same way for a missing run: the live endpoint
  // falls back to the settled diff rather than inventing an empty one.
  rec('P13', 'the live diff endpoint falls back to the settled diff',
    shape.live === shape.settled, `live=${shape.live} settled=${shape.settled}`);
}

await browser.close();

console.log(results.join('\n'));
const pass = results.filter(r => r.includes('PASS')).length;
const fail = results.filter(r => r.includes('FAIL')).length;
const skip = results.filter(r => r.includes('SKIP')).length;
console.log(`\n=== ${pass} PASS / ${fail} FAIL / ${skip} SKIP of ${n} ===`);
if (errors.length) {
  console.log('\n--- errors (' + errors.length + ') ---');
  console.log(errors.join('\n'));
}
process.exit(fail ? 1 : 0);

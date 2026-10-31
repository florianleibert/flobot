// verify-editor-overhaul.mjs — browser audit for the editor overhaul:
// open editors (tabs), find & replace, and workspace text search.
//
// Run against a live flobot server:
//   FLOBOT_URL=http://127.0.0.1:4120 node docs/audit/verify-editor-overhaul.mjs
import { createRequire } from "node:module";

// Playwright is resolved through CommonJS so NODE_PATH works: the other
// audits in this folder assume the same setup.
const require = createRequire(import.meta.url);
const { chromium } = require("playwright");

const base = process.env.FLOBOT_URL || "http://127.0.0.1:4120";
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 920 } });
const errors = [];
page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
page.on("console", (m) => {
  if (m.type() === "error") errors.push("console: " + m.text());
});

let pass = 0;
let fail = 0;
const check = (name, ok, detail) => {
  if (ok) {
    pass++;
    console.log("PASS [" + name + "]" + (detail ? " — " + detail : ""));
  } else {
    fail++;
    console.log("FAIL [" + name + "]" + (detail ? " — " + detail : ""));
  }
};

const openPath = async (p) => {
  await page.fill("#edPath", p);
  await page.click("#edOpen");
  await page.waitForTimeout(700);
};

await page.goto(base, { waitUntil: "domcontentloaded" });
await page.click('button[data-panel="editor"]');
await page.waitForSelector("#edOpenTabs", { timeout: 10000 });
await page.evaluate(() => localStorage.removeItem("edTabs"));
await page.waitForTimeout(400);

// P1 — opening two files gives two editors.
await openPath("game.js");
await openPath("hello.txt");
const tabs = await page.locator(".ed-opentab").count();
check("P1 two open editors", tabs === 2, tabs + " tabs");

// P2 — the active editor is the one just opened.
const active = await page.evaluate(() => document.getElementById("edPath").value);
check("P2 active editor follows the open", active === "hello.txt", active);

// P3 — switching tabs swaps the buffer.
await page.click('.ed-opentab:has-text("game.js")');
await page.waitForTimeout(500);
const buf = await page.evaluate(() => document.getElementById("edPath").value);
check("P3 tab switch swaps the buffer", buf === "game.js", buf);

// P4 — an edit marks the editor dirty.
await page.click("#edContent");
await page.keyboard.type("// edited by the audit\n");
await page.waitForTimeout(400);
const dirty = await page.locator(".ed-opentab-dirty").count();
check("P4 edit marks the editor dirty", dirty >= 1, dirty + " dirty markers");

// P5 — the edit survives the tab switch.
await page.click('.ed-opentab:has-text("hello.txt")');
await page.waitForTimeout(400);
await page.click('.ed-opentab:has-text("game.js")');
await page.waitForTimeout(500);
const kept = await page.evaluate(() => document.getElementById("edContent").value.includes("edited by the audit"));
check("P5 the edit survives the tab switch", kept);

// P6 — find & replace opens on Cmd+F.
await page.keyboard.press("Meta+f");
await page.waitForTimeout(300);
const barOpen = await page.evaluate(() => !document.getElementById("edFind").hidden);
check("P6 find bar opens", barOpen);

// P7 — find counts matches.
await page.fill("#edFindInput", "function");
await page.waitForTimeout(400);
const count = await page.evaluate(() => document.getElementById("edFindCount").textContent);
check("P7 find counts matches", /\d+ of \d+/.test(count), count);

// P8 — replace changes the buffer.
await page.fill("#edReplaceInput", "async function");
await page.click("#edFindReplaceAll");
await page.waitForTimeout(400);
const replaced = await page.evaluate(() => document.getElementById("edContent").value.includes("async function"));
check("P8 replace edits the buffer", replaced);

// P9 — workspace text search finds hits.
await page.fill("#edSearchInput", "game");
await page.waitForTimeout(900);
const hits = await page.locator(".ed-search-item").count();
check("P9 workspace search finds hits", hits > 0, hits + " hits");

// P10 — clicking a hit opens that file.
if (hits > 0) {
  await page.locator(".ed-search-item").first().click();
  await page.waitForTimeout(800);
  const opened = await page.evaluate(() => document.getElementById("edPath").value);
  check("P10 a search hit opens the file", !!opened, opened);
}

// P11 — the open editors survive a reload (hot exit).
const before = await page.locator(".ed-opentab").count();
await page.reload({ waitUntil: "domcontentloaded" });
await page.click('button[data-panel="editor"]');
await page.waitForTimeout(1500);
const after = await page.locator(".ed-opentab").count();
check("P11 open editors survive a reload", after >= 1, before + " before, " + after + " after");

// P12 — no page errors.
check("P12 no page errors", errors.length === 0, errors.join(" | "));

await page.screenshot({ path: "/tmp/flobot-editor.png" });
console.log("TOTAL " + pass + " PASS / " + fail + " FAIL");
await browser.close();
process.exit(fail ? 1 : 0);

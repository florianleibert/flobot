// verify-projects.mjs — browser audit for the project/tag Code view.
//
// Run against a live flobot server:
//   FLOBOT_URL=http://127.0.0.1:4120 node docs/audit/verify-projects.mjs
//
// The Code view is organised by projects and tags instead of by a raw file
// tree: a project names a folder plus the files in scope, tags label
// projects (Apple Photos style), and an agent can be pointed at a project or
// at a tag so the mission carries its scope.
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

await page.goto(base, { waitUntil: "domcontentloaded" });
await page.click('button[data-panel="editor"]');
await page.waitForSelector("#edProjects", { timeout: 10000 });
await page.waitForTimeout(700);

// P1 — the projects block lives inside the Code panel's left rail.
check("P1 projects block renders", (await page.locator(".ed-projects-block").count()) === 1);
const children = await page.locator(".ed-layout > *").count();
check("P1b layout keeps 3 grid columns", children === 3, children + " grid children");
const box = await page.evaluate(() => {
  const block = document.querySelector(".ed-projects-block");
  const p = block.getBoundingClientRect();
  const t = document.querySelector(".ed-tree-card").getBoundingClientRect();
  const m = document.querySelector(".ed-main-card").getBoundingClientRect();
  return { p, t, m, inside: !!block.closest(".ed-tree-card") };
});
check(
  "P1c projects live in the left rail, above the file tree",
  box.inside && box.p.top >= box.t.top && box.p.left < box.m.left,
  "inside=" + box.inside
);

// P2 — projects are listed as cards.
const cards = await page.locator(".ed-project").count();
check("P2 project cards listed", cards > 0, cards + " cards");

// P3 — the tag strip shows the vocabulary with counts.
const tagTexts = await page.locator("#edTags .ed-tag").allTextContents();
check("P3 tag strip shows tags", tagTexts.length > 1, JSON.stringify(tagTexts));

// P4 — clicking a tag filters the project list.
const firstTag = tagTexts.find((t) => t.startsWith("#") && !t.includes("· 0"));
if (firstTag) {
  await page.click(`#edTags .ed-tag:has-text("${firstTag.replace(/"/g, "")}")`);
  await page.waitForTimeout(400);
  const filtered = await page.locator(".ed-project").count();
  check("P4 tag filters projects", filtered >= 1 && filtered <= cards, filtered + " after filtering by " + firstTag);
  await page.click('#edTags .ed-tag:has-text("all")');
  await page.waitForTimeout(300);
}

// P5 — the scope editor lists files.
await page.locator(".ed-project .ed-btn:has-text('files')").first().click();
await page.waitForTimeout(600);
const scopeItems = await page.locator(".ed-scope-item").count();
check("P5 scope editor lists files", scopeItems > 0, scopeItems + " entries");

// P6 — "work on this" sets the run scope.
await page.locator(".ed-project .ed-btn:has-text('work on this')").first().click();
await page.waitForTimeout(300);
const scope = await page.evaluate(() => window.codeStudioScope || null);
check("P6 work sets the run scope", !!(scope && (scope.project || scope.tag)), JSON.stringify(scope));

// P7 — no page errors while using the panel.
check("P7 no page errors", errors.length === 0, errors.join(" | "));

await page.screenshot({ path: "/tmp/flobot-projects.png" });
console.log("TOTAL " + pass + " PASS / " + fail + " FAIL");
await browser.close();
process.exit(fail ? 1 : 0);

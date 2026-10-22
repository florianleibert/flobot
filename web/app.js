// flobot web UI — mission control for local agents.
// Connects to /api/stream (NDJSON) and renders every event kind:
// content, thinking (collapsed), tool_start/tool_end (paired),
// perm (mock ack), plan, err, done, verify (badge).
//
// Security: the UI never displays secrets or API keys. Every string that
// reaches the DOM passes through sanitize() first, which masks the
// documented key file (~/.accounts/meta.ai) and any MODEL_API_KEY
// material, and esc() HTML-escapes the result.

const $ = id => document.getElementById(id);
let roster = {bots:[], skills:[], routines:[], messages:[], routes:[], runs:[]};
let activeRun = null;
let pollTimer = null;
let elapsedTimer = null;
let runStart = null;

// ---- stream state -------------------------------------------------------

let streamES = null;
let streamBuf = "";
let streamOpen = false;
let streamRetries = 0;
let streamTimer = null;
let streamRunId = null;

// ---- secret sanitization ------------------------------------------------

// secretPatterns mask anything that looks like credential material before
// it can be rendered. They are deliberately broad: better to over-mask.
const secretPatterns = [
  /MODEL_API_KEY\s*[=:]\s*\S+/g,
  /sk-[A-Za-z0-9_\-]{8,}/g,
  /Bearer\s+[A-Za-z0-9_\-\.]{8,}/g,
  /~\/\.accounts\/meta\.ai/g,
  /\/\.accounts\/meta\.ai/g,
];

function sanitize(s) {
  let out = String(s ?? "");
  for (const re of secretPatterns) out = out.replace(re, "[REDACTED]");
  return out;
}

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
}

// safe renders a value for the DOM: sanitize first, then HTML-escape.
function safe(s) { return esc(sanitize(s)); }

// ---- toasts (interaction feedback) --------------------------------------

function toast(msg, cls) {
  const host = $("toasts");
  // Cap concurrent toasts so a burst of events can't stack a column of
  // them over the UI; oldest fade out immediately.
  while (host.children.length >= 4) host.firstChild.remove();
  const t = document.createElement("div");
  t.className = "toast" + (cls ? " " + cls : "");
  t.textContent = msg;
  host.appendChild(t);
  setTimeout(() => { t.style.opacity = "0"; t.style.transition = "opacity .3s"; }, 3200);
  setTimeout(() => t.remove(), 3600);
}

function showErr(e) {
  if (!e) return;
  toast(e, "err");
}

// ---- API ----------------------------------------------------------------

async function api(method, path, body) {
  const opt = {method, headers:{}};
  if (body !== undefined) {
    opt.headers["Content-Type"] = "application/json";
    opt.body = JSON.stringify(body);
  }
  const r = await fetch(path, opt);
  const text = await r.text();
  if (!r.ok) {
    // Prefer the parsed error message over the raw JSON body.
    let msg = text.trim() || r.statusText;
    try { const j = JSON.parse(text); if (j && j.error) msg = j.error; } catch {}
    throw new Error(msg);
  }
  if (!text) return null;
  try { return JSON.parse(text); } catch { return text; }
}

function fillSelect(el, items, label) {
  const cur = el.value;
  const html = items.map(b => `<option value="${esc(b.id)}">${esc(label(b))}</option>`).join("");
  if (el.innerHTML !== html) el.innerHTML = html;
  if ([...el.options].some(o => o.value === cur)) el.value = cur;
}

// fillRouteSelect renders the documented routes from the live roster plus
// any ready mesh models as options for the bot route. Fully reflective: no
// hardcoded ids — whatever the server reports is what you can pin.
function fillRouteSelect(el, mesh, direct) {
  const cur = el.value;
  const opts = (roster.routes || []).map(r =>
    `<option value="${esc(r.id)}">${esc(r.id)}</option>`).join("");
  const meshOpts = (mesh || []).map(m =>
    `<option value="${esc(m.route)}">${esc(m.route)}${m.display ? " · " + esc(m.display) : ""}</option>`).join("");
  const directOpts = (direct || []).map(m =>
    `<option value="${esc(m.route)}">${esc(m.route)}${m.display ? " · " + esc(m.display) : ""}</option>`).join("");
  const html = opts + meshOpts + directOpts;
  if (el.innerHTML !== html) el.innerHTML = html;
  if ([...el.options].some(o => o.value === cur)) el.value = cur;
}

// ---- navigation -----------------------------------------------------------

const nav = $("nav");
nav.querySelectorAll("button[data-panel]").forEach(btn => {
  btn.onclick = () => {
    nav.querySelectorAll("button").forEach(b => b.classList.remove("active"));
    btn.classList.add("active");
    document.querySelectorAll(".panel").forEach(p => p.classList.remove("active"));
    const p = $("panel-" + btn.dataset.panel);
    if (p) p.classList.add("active");
    // lazy-init the Code Studio editor panel
    if (btn.dataset.panel === "editor" && window.codeStudioInit) window.codeStudioInit();
    const title = $("panelTitle");
    if (title && p) {
      title.textContent = p.dataset.title || p.querySelector("h2")?.textContent || "";
      // restart the pop animation so each switch feels deliberate
      title.classList.remove("pop");
      void title.offsetWidth;
      title.classList.add("pop");
    }
  };
});

function gotoPanel(name) {
  const btn = nav.querySelector(`button[data-panel="${name}"]`);
  if (btn) btn.click();
}

// ---- rendering ----------------------------------------------------------

let meshModels = [];
let directModels = [];

// animateNum tweens a stat number to its target with an ease-out cubic,
// so live updates feel like instruments rather than teletypes.
function animateNum(el, target) {
  if (!el) return;
  const from = parseInt(el.dataset.v || "0", 10) || 0;
  if (from === target) { el.textContent = target; el.dataset.v = String(target); return; }
  const t0 = performance.now();
  const dur = 500;
  const step = now => {
    const p = Math.min((now - t0) / dur, 1);
    const eased = 1 - Math.pow(1 - p, 3);
    el.textContent = Math.round(from + (target - from) * eased);
    if (p < 1) requestAnimationFrame(step);
    else { el.textContent = target; el.dataset.v = String(target); }
  };
  requestAnimationFrame(step);
}

// avatarColor picks a stable hue from a string.
function avatarColor(s) {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
  return `hsl(${h % 360} 55% 62%)`;
}

function avatar(name) {
  const init = (name || "?").trim().slice(0, 2).toUpperCase();
  return `<span class="avatar" style="background:${avatarColor(name)}">${esc(init)}</span>`;
}

function empty(icon, text) {
  return `<li class="empty"><span class="ico">${icon}</span>${esc(text)}</li>`;
}

// skeleton renders content-shaped loading placeholders (never a spinner),
// matching the shape of the list that is about to arrive.
function skeleton(n) {
  return Array.from({length: n}, () =>
    `<li class="skel"><span class="skel-av"></span><div class="skel-body"><span class="skel-line w60"></span><span class="skel-line w90"></span></div></li>`
  ).join("");
}

// setHTML writes innerHTML only when the content actually changed. Full
// innerHTML rewrites on every refresh were the main source of list flicker:
// the browser tears down and relayouts identical DOM. Skipping no-op
// writes makes periodic refreshes invisible.
function setHTML(el, html) {
  if (!el || el.innerHTML === html) return;
  el.innerHTML = html;
}

function render() {
  // nav counts + stats
  $("navBots").textContent = roster.bots.length;
  $("navRuns").textContent = (roster.runs||[]).length;
  $("navRtn").textContent = roster.routines.length;
  $("navSkills").textContent = roster.skills.length;

  // stats tween so live changes read as motion
  animateNum($("statBots"), roster.bots.length);
  animateNum($("statRoutines"), roster.routines.length);
  animateNum($("statRuns"), (roster.runs||[]).length);
  animateNum($("statMesh"), meshModels.length);

  // roster
  setHTML($("bots"), roster.bots.map(b =>
    `<li>${avatar(b.name)}
      <div class="body">
        <div class="title">${esc(b.name)} <span class="chip acc">${esc(b.role)}</span></div>
        <div class="clip">${esc(b.route)}${b.memory ? " · remembers: " + safe(b.memory) : ""}</div>
      </div>
      <div class="actions"><span class="tag">${esc(b.id)}</span>
        <button class="ghost danger" data-delbot="${esc(b.id)}" title="Delete bot">✕</button></div></li>`
  ).join("") || empty("◍", "No teammates yet — add one above."));
  $("bots").querySelectorAll("[data-delbot]").forEach(btn => btn.onclick = () => {
    if (confirm("Delete this bot? Its routines go too; runs are kept.")) delBot(btn.dataset.delbot);
  });

  fillSelect($("runBot"), roster.bots, b => `${b.name} (${b.route})`);
  fillSelect($("qBot"), roster.bots, b => `${b.name} (${b.route})`);
  fillSelect($("qMemBot"), roster.bots, b => b.name);
  fillSelect($("rtnBot"), roster.bots, b => b.name);
  fillSelect($("fromBot"), roster.bots, b => b.name);
  fillSelect($("toBot"), roster.bots, b => b.name);
  fillSelect($("rtnSkill"), roster.skills, s => s.name);
  fillSelect($("memBot"), roster.bots, b => b.name);
  fillRouteSelect($("botRoute"), meshModels, directModels);
  $("handoff").style.display = roster.bots.length >= 2 ? "block" : "none";
  setHTML($("msgs"), (roster.messages||[]).map(m =>
    `<li>${avatar(m.from)}
      <div class="body">
        <div class="title">${esc(m.from)} → ${esc(m.to)} <span class="tag">owner ${esc(m.owner_bot)}</span></div>
        <div class="clip">${safe(m.body)}</div>
      </div></li>`
  ).join("") || empty("⇄", "No handoffs yet."));

  setHTML($("skills"), roster.skills.map(s =>
    `<li><span class="avatar" style="background:var(--acc-soft);color:var(--acc)">✦</span>
      <div class="body"><div class="title">${esc(s.name)}</div><div class="clip">${esc(s.id)}</div></div>
      <div class="actions"><button class="ghost danger" data-delskill="${esc(s.id)}" title="Delete skill">✕</button></div></li>`
  ).join("") || empty("✦", "No skills yet — write one above."));
  $("skills").querySelectorAll("[data-delskill]").forEach(btn => btn.onclick = () => {
    if (confirm("Delete this skill? Routines using it go too.")) delSkill(btn.dataset.delskill);
  });

  setHTML($("routines"), roster.routines.map(rt => {
    const state = rt.paused ? `<span class="chip warn">paused</span>` : `<span class="chip ok">on</span>`;
    return `<li><span class="avatar" style="background:var(--acc-soft);color:var(--acc)">↻</span>
      <div class="body">
        <div class="title">${esc(rt.trigger)} ${state}</div>
        <div class="clip">${esc(rt.id)} · ${esc(rt.last_note||"no test-run")}</div>
      </div>
      <div class="actions">
        <button class="ghost" data-test="${esc(rt.id)}">test-run</button>
        <button class="ghost" data-pause="${esc(rt.id)}">${rt.paused ? "resume" : "pause"}</button>
        <button class="ghost danger" data-delrtn="${esc(rt.id)}" title="Delete routine">✕</button>
      </div></li>`;
  }).join("") || empty("↻", "No routines yet — schedule one above."));
  $("routines").querySelectorAll("[data-test]").forEach(btn => btn.onclick = () => testRun(btn.dataset.test));
  $("routines").querySelectorAll("[data-pause]").forEach(btn => btn.onclick = () => pauseRtn(btn.dataset.pause, !btn.textContent.includes("resume")));
  $("routines").querySelectorAll("[data-delrtn]").forEach(btn => btn.onclick = () => {
    if (confirm("Delete this routine?")) delRtn(btn.dataset.delrtn);
  });

  const tb = $("routes").querySelector("tbody");
  setHTML(tb, (roster.routes||[]).map(r =>
    `<tr><td>${esc(r.id)}</td><td>${esc(r.role)}</td><td>${esc(r.model)}</td><td class="tag">${esc(r.purpose)}</td></tr>`
  ).join(""));

  // Run history: newest first, capped at 20 rows. Clicking a run replays
  // its events into the log (Grok Bot-style run visibility).
  const botName = {};
  for (const b of roster.bots) botName[b.id] = b.name;
  setHTML($("runHistory"), (roster.runs||[]).slice(0, 20).map(r => {
    const chip = r.status === "done" ? "ok" : (r.status === "running" ? "acc" : (r.status === "stopped" ? "warn" : "err"));
    const dur = runDuration(r);
    // AC-2f: show the run's diff and review state when a summary exists.
    const ds = r.diffSummary;
    const diffTag = ds && (ds.files || ds.additions || ds.deletions)
      ? ` <span class="tag" title="${ds.files} files">+${esc(ds.additions||0)}/−${esc(ds.deletions||0)}</span>` : "";
    const revTag = r.reviewState && r.reviewState !== "pending"
      ? ` <span class="chip ${r.reviewState === "accepted" ? "ok" : r.reviewState === "partial" ? "acc" : "warn"}">${esc(r.reviewState)}</span>` : "";
    return `<li class="runrow" data-runrow="${esc(r.id)}">
      <div class="body">
        <div class="title"><span class="runid">${esc(r.id.slice(0,8))}</span> <span class="chip ${chip}">${esc(r.status)}</span>${dur ? ` <span class="tag">${esc(dur)}</span>` : ""}${diffTag}${revTag}</div>
        <div class="clip">${esc(botName[r.bot_id] || r.bot_id || "—")} · ${esc(r.intent)} · ${esc(relTime(r.created_at))} · ${esc((r.prompt||"").slice(0,80))}${r.error ? " · " + esc(r.error) : ""}</div>
      </div></li>`;
  }).join("")
    || `<li class="empty"><span class="ico">▶</span>No runs yet — start one with <b>r</b> or the Run panel.</li>`);
  $("runHistory").querySelectorAll("[data-runrow]").forEach(li =>
    li.onclick = () => { openDrawer(li.dataset.runrow).catch(e => showErr(e.message)); });
}

// showSkeletons fills the primary lists with loading placeholders until the
// first roster arrives, so first paint never flashes a blank box.
function showSkeletons() {
  $("bots").innerHTML = skeleton(3);
  $("runHistory").innerHTML = skeleton(2);
  $("msgs").innerHTML = skeleton(2);
}

async function refresh() {
  roster = await api("GET", "/api/v1/roster");
  render();
}

// loadMesh fetches the ready amesh mesh models and renders them as
// selectable routes. An amesh-web that is down is a rendered state.
async function loadMesh() {
  try {
    const res = await api("GET", "/api/v1/mesh/models");
    meshModels = res.ok ? (res.models || []) : [];
    $("meshCount").textContent = res.ok
      ? `${meshModels.length} ready`
      : `unavailable (${res.reason || "amesh-web unreachable"})`;
  } catch (e) {
    meshModels = [];
    $("meshCount").textContent = `unavailable (${e.message})`;
  }
  $("meshModels").innerHTML = meshModels.map(m =>
    `<li><span class="avatar" style="background:var(--ok-soft);color:var(--ok)">◈</span>
      <div class="body">
        <div class="title">${esc(m.route)}</div>
        <div class="clip">${esc(m.display || m.model)}${m.context_tokens ? " · " + m.context_tokens + " ctx" : ""}</div>
      </div></li>`
  ).join("") || empty("◈", "No mesh models ready.");
  render();
}

// loadDirect fetches the models available on the Meta model API (fetched
// server-side; the key never reaches the browser) and renders them as
// direct/<model> routes. This is the fallback when amesh is down and the
// way to force a direct route.
async function loadDirect() {
  try {
    const res = await api("GET", "/api/v1/direct/models");
    directModels = res.ok ? (res.models || []) : [];
    $("directCount").textContent = res.ok
      ? `${directModels.length} available`
      : `unavailable (${res.reason || "no key"})`;
  } catch (e) {
    directModels = [];
    $("directCount").textContent = `unavailable (${e.message})`;
  }
  $("directModels").innerHTML = directModels.map(m =>
    `<li><span class="avatar" style="background:var(--ok-soft);color:var(--ok)">◆</span>
      <div class="body">
        <div class="title">${esc(m.route)}</div>
        <div class="clip">${esc(m.display || m.model)}</div>
      </div></li>`
  ).join("") || empty("◆", "No direct models available.");
  render();
}

// ---- event rendering ----------------------------------------------------

// openTools tracks tool_start events awaiting their tool_end so the UI can
// pair them and mark the tool as running until it finishes.
const openTools = new Map();

// event filter state — one active kind per console; "all" shows everything.
let evFilter = "all";
let drawerFilter = "all";

// kindOf maps an event to its filter bucket. tool_start/tool_end collapse
// into "tool" so the chip reads naturally.
function kindOf(e) {
  const k = e.kind || "";
  if (k === "tool_start" || k === "tool_end") return "tool";
  return k;
}

// matchesFilter decides whether an event passes the active filter.
function matchesFilter(e, filter) {
  if (!filter || filter === "all") return true;
  return kindOf(e) === filter;
}

// applyFilter re-renders the live log under the current filter. It replays
// the buffered events so filtering feels instant, not append-only.
let logEvents = [];   // buffered events for the live console (filter replay)
function applyFilter() {
  const log = logEl();
  log.innerHTML = "";
  openTools.clear();
  for (const e of logEvents) {
    if (matchesFilter(e, evFilter)) appendEvent(e);
  }
  log.scrollTop = log.scrollHeight;
}

function wireFilters(container, setter) {
  container.querySelectorAll(".chipf").forEach(chip => {
    chip.onclick = () => {
      container.querySelectorAll(".chipf").forEach(c => c.classList.remove("active"));
      chip.classList.add("active");
      setter(chip.dataset.kind);
    };
  });
}
wireFilters($("evFilters"), k => { evFilter = k; applyFilter(); });
wireFilters($("drawerFilters"), k => { drawerFilter = k; renderDrawerLog(); });

function logEl() { return $("log"); }

// nearBottom reports whether the log is scrolled to (or near) the bottom,
// i.e. the user is following the stream. Autoscroll only then — never yank
// the view while someone is scrolled up reading history.
function nearBottom(el) {
  return el.scrollHeight - el.scrollTop - el.clientHeight < 40;
}

function appendEvent(e) {
  const log = logEl();
  const stick = nearBottom(log);
  log.appendChild(buildEventNode(e));
  // A run is terminal on "done" or on a run_status of done/error/stopped;
  // all three must refresh the roster so RECENT RUNS never goes stale.
  if (e.kind === "done" || (e.kind === "run_status" && /^(done|error|stopped)$/.test(e.text || ""))) {
    onRunEnd(e.kind === "run_status" ? e.text : "done");
  }
  if (stick) log.scrollTop = log.scrollHeight;
  // Code Studio: breathe the review surface while a run is in flight,
  // settle it when the run ends, and load the diff when it's ready.
  const review = document.getElementById("edReview");
  if (review) {
    if (e.kind === "run_status" && /^(running|queued)$/.test(e.text || "")) {
      review.classList.add("live");
    } else if (e.kind === "run_status" && /^(done|error|stopped)$/.test(e.text || "")) {
      review.classList.remove("live");
    } else if (e.kind === "diff_ready") {
      if (window.codeStudio) {
        window.codeStudio.openRunDiff(e.text);
      } else {
        // Editor panel not yet opened: stash the pending run so
        // codeStudioInit can consume it when the user opens the tab.
        window.codeStudioPendingRun = e.text;
      }
    } else if (e.kind === "review_done") {
      // A review finished (possibly in another tab/window): refresh
      // the tree and checkpoints so the editor does not show stale
      // workspace state.
      if (window.codeStudio) window.codeStudio.refresh();
    }
  }
}

// buildEventNode renders one event through the rich renderers (paired tools,
// collapsed thinking, perm ack, verify badge). The live console and the
// run-detail drawer share this path so both transcripts look identical.
function buildEventNode(e, tools) {
  const kind = e.kind || "";
  switch (kind) {
    case "content":
      return evDiv("content", "content", e.text);
    case "thinking":
      return thinkingEl(e.text);
    case "tool_start":
      return toolEl(e, false, tools);
    case "tool_end":
      return toolEl(e, true, tools);
    case "perm":
      return permEl(e);
    case "plan":
      return evDiv("plan", "plan", e.text);
    case "err":
      return evDiv("err", "err", e.text);
    case "done":
      return evDiv("done", "done", e.text);
    case "verify":
      return verifyEl(e);
    case "run_status":
      return evDiv("run_status", "run_status", e.text);
    case "result_contract":
      return resultContractEl(e.text);
    case "diff_ready":
      return evDiv("done", "diff", "diff ready — review in Editor");
    case "routine_fire":
      return evDiv("routine_fire", "routine_fire", "routine " + e.text + " fired");
    default:
      return evDiv("unknown", kind, e.text);
  }
}

function evDiv(cls, label, text) {
  const d = document.createElement("div");
  d.className = "ev " + cls;
  const k = document.createElement("span");
  k.className = "k";
  k.textContent = label;
  d.appendChild(k);
  if (text) d.appendChild(document.createTextNode(" " + sanitize(text)));
  return d;
}

// thinkingEl renders a thinking event as a collapsed <details> block.
function thinkingEl(text) {
  const d = document.createElement("details");
  d.className = "ev thinking";
  const s = document.createElement("summary");
  s.className = "k";
  s.textContent = "thinking";
  d.appendChild(s);
  const body = document.createElement("div");
  body.className = "body";
  body.textContent = sanitize(text || "");
  d.appendChild(body);
  return d;
}

// toolEl renders tool_start/tool_end. A tool_start opens a row that stays
// "running" until its matching tool_end (same tool name, FIFO) arrives.
// tools is an optional pairing map; it defaults to the live-console map so
// the drawer can keep its own state without disturbing streaming runs.
function toolEl(e, isEnd, tools) {
  tools = tools || openTools;
  const tool = e.tool || "tool";
  const d = document.createElement("div");
  d.className = "ev tool " + (isEnd ? "tool_end" : "tool_start");
  const k = document.createElement("span");
  k.className = "k";
  k.textContent = (isEnd ? "tool_end" : "tool_start") + " " + tool;
  d.appendChild(k);

  if (!isEnd) {
    const st = document.createElement("span");
    st.className = "tool-status";
    st.textContent = " running";
    d.appendChild(st);
    tools.set(tool, d);
  } else {
    // Pair with the most recent open start for this tool.
    const open = tools.get(tool);
    if (open) {
      const st = open.querySelector(".tool-status");
      if (st) st.textContent = e.is_error ? " failed" : " done";
      open.classList.add("closed");
      if (e.is_error) open.classList.add("err");
      tools.delete(tool);
    }
    if (e.is_error) d.classList.add("err");
    if (e.text) d.appendChild(document.createTextNode(" " + sanitize(e.text)));
  }
  return d;
}

// permEl renders a permission request with a mock acknowledgment button.
// The ack is local-only: it marks the event acknowledged in the UI.
function permEl(e) {
  const d = document.createElement("div");
  d.className = "ev perm";
  const k = document.createElement("span");
  k.className = "k";
  k.textContent = "perm";
  d.appendChild(k);
  if (e.text) d.appendChild(document.createTextNode(" " + sanitize(e.text)));
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = "ack";
  btn.textContent = "ack";
  btn.onclick = () => {
    btn.textContent = "acknowledged";
    btn.disabled = true;
    d.classList.add("acked");
  };
  d.appendChild(btn);
  return d;
}

// verifyEl renders the verify event as a success/failure badge.
function verifyEl(e) {
  const d = document.createElement("div");
  const ok = !e.is_error;
  d.className = "ev verify " + (ok ? "ok" : "fail");
  const badge = document.createElement("span");
  badge.className = "badge";
  badge.textContent = ok ? "verify ✓" : "verify ✗";
  d.appendChild(badge);
  if (e.text) d.appendChild(document.createTextNode(" " + sanitize(e.text)));
  return d;
}

// onRunEnd fires when a terminal event (done) is seen: stop the
// onRunEnd stops the elapsed timer and refreshes the roster so run history
// updates. It also hides the stop button — the run is over.
// status is the terminal status ("done" | "error" | "stopped"); it is
// forwarded to the Electron shell for the completion notification.
function onRunEnd(status) {
  stopElapsed();
  const sb = $("stopBtn");
  if (sb) {
    sb.hidden = true;
    // Reset in case a missed terminal event left it disabled at "stopping…".
    sb.disabled = false;
    sb.textContent = "Stop";
  }
  // Code Studio AC-2e: ask the Electron shell for a completion
  // notification. The main process drops it when the window is focused,
  // so this is a no-op while the user is watching the run.
  try {
    if (window.flobotDesktop && window.flobotDesktop.notifyRunEnd) {
      window.flobotDesktop.notifyRunEnd({ status: status || "done" });
    }
  } catch {}
  refresh().catch(() => {});
}

function clearLog() {
  logEl().innerHTML = "";
  openTools.clear();
  logEvents = [];
}

// ---- elapsed timer -------------------------------------------------------

function startElapsed() {
  runStart = Date.now();
  stopElapsed();
  const tick = () => {
    if (!runStart) return;
    const s = Math.floor((Date.now() - runStart) / 1000);
    const el = $("elapsed");
    if (el) el.textContent = s < 60 ? s + "s" : Math.floor(s/60) + "m " + (s%60) + "s";
  };
  tick();
  elapsedTimer = setInterval(tick, 1000);
}
function stopElapsed() {
  if (elapsedTimer) { clearInterval(elapsedTimer); elapsedTimer = null; }
}

// ---- /api/stream client -------------------------------------------------

// handleEvent parses one NDJSON line and renders it.
function handleEvent(line) {
  line = line.trim();
  if (!line) return;
  let ev;
  try { ev = JSON.parse(line); } catch { return; }
  if (!ev || typeof ev !== "object") return;
  logEvents.push(ev);
  if (matchesFilter(ev, evFilter)) appendEvent(ev);
}

// feedStreamLine appends a chunk to the buffer and emits complete lines.
function feedStreamLine(chunk) {
  streamBuf += chunk;
  let idx;
  while ((idx = streamBuf.indexOf("\n")) >= 0) {
    const line = streamBuf.slice(0, idx);
    streamBuf = streamBuf.slice(idx + 1);
    handleEvent(line);
  }
}

// connectStream opens the NDJSON stream. It reconnects with backoff and,
// when a run is active, resumes from the last event it rendered (?since=)
// so a reconnect appends only the gap instead of replaying duplicates.
function connectStream() {
  if (streamES) { try { streamES.close(); } catch {} streamES = null; }
  let url = "/api/stream";
  if (streamRunId) {
    url += "?run=" + encodeURIComponent(streamRunId);
    if (logEvents.length) url += "&since=" + logEvents.length;
  }
  const es = new EventSource(url);
  streamES = es;
  es.onopen = () => { streamOpen = true; streamRetries = 0; setStreamState("live"); };
  es.onmessage = m => feedStreamLine(m.data);
  es.onerror = () => {
    streamOpen = false;
    setStreamState("reconnecting");
    try { es.close(); } catch {}
    streamES = null;
    streamRetries++;
    const delay = Math.min(1000 * 2 ** streamRetries, 15000);
    streamTimer = setTimeout(connectStream, delay);
  };
}

function setStreamState(state) {
  const el = $("streamState");
  const pill = $("streamPill");
  if (el) el.textContent = state;
  if (pill) {
    pill.classList.remove("live", "reconnecting");
    if (state === "live" || state === "reconnecting") pill.classList.add(state);
  }
}

// stopRun cancels the active run's executor context server-side. The
// stream then delivers the run_status stopped event and onRunEnd cleans up.
async function stopRun() {
  const id = streamRunId || activeRun;
  if (!id) return;
  const btn = $("stopBtn");
  if (btn) { btn.disabled = true; btn.textContent = "stopping…"; }
  try {
    await api("POST", "/api/v1/runs/" + id + "/stop");
    toast("run stopped", "ok");
  } catch (e) {
    showErr(e.message);
    if (btn) { btn.disabled = false; btn.textContent = "\u25a0 stop"; }
  }
}

// startStream begins streaming for a run (or the global feed if id is null).
function startStream(runId) {
  streamRunId = runId || null;
  const sb = $("stopBtn");
  if (sb) { sb.hidden = !runId; sb.disabled = false; sb.textContent = "\u25a0 stop"; }
  if (streamTimer) { clearTimeout(streamTimer); streamTimer = null; }
  streamBuf = "";
  connectStream();
  if (runId) startElapsed(); else { runStart = null; stopElapsed(); }
}

// stopStream tears down the stream client.
function stopStream() {
  if (streamTimer) { clearTimeout(streamTimer); streamTimer = null; }
  if (streamES) { try { streamES.close(); } catch {} streamES = null; }
  streamOpen = false;
  streamRunId = null;
  const sb = $("stopBtn");
  if (sb) sb.hidden = true;
  setStreamState("idle");
}

// ---- polling fallback ---------------------------------------------------

function stopPoll() { if (pollTimer) { clearInterval(pollTimer); pollTimer = null; } }

async function pollOnce() {
  if (!activeRun) return;
  const ev = await api("GET", "/api/v1/runs/"+activeRun+"/events");
  // Append only the events we have not rendered yet instead of rebuilding
  // the whole log every 500ms — the rebuild was the log's flicker source.
  const all = ev.events || [];
  if (all.length < logEvents.length) {
    // Fewer events than rendered (run restarted / replay reset): full redraw.
    clearLog();
  }
  for (const e of all.slice(logEvents.length)) {
    logEvents.push(e);
    if (matchesFilter(e, evFilter)) appendEvent(e);
  }
  if (ev.status && ev.status !== "running") {
    stopPoll();
    activeRun = null;
    await refresh();
  }
}

function startPoll(id) {
  activeRun = id;
  stopPoll();
  pollOnce().catch(e => showErr(e.message));
  pollTimer = setInterval(() => pollOnce().catch(e => showErr(e.message)), 500);
}

// ---- run detail drawer ---------------------------------------------------

let drawerRunId = null;

// detHash is a small deterministic integer hash so sparkline bars keep a
// stable organic shape across re-renders instead of reshuffling randomly.
function detHash(i) {
  let x = ((i * 2654435761) >>> 0) ^ ((i << 13) >>> 0);
  x ^= x >>> 13;
  x = (x * 1274126177) >>> 0;
  return x % 18;
}

// sparkline renders one bar per event bucket, height derived deterministically
// from position + kind so filtering or reopening never changes the shape.
function sparkline(events) {
  const el = $("drawerSpark");
  if (!events || !events.length) { el.innerHTML = ""; return; }
  const kinds = events.map(kindOf);
  const buckets = [];
  const n = Math.min(events.length, 90);
  const step = Math.max(1, Math.floor(events.length / n));
  for (let i = 0; i < events.length; i += step) {
    buckets.push(kinds.slice(i, i + step));
  }
  el.innerHTML = buckets.map((bucket, bi) => {
    const k = bucket[0];
    const cls = k === "tool" ? "k-tool" : k === "err" ? "k-err" : k === "done" ? "k-done"
      : k === "verify" ? "k-verify" : k === "thinking" ? "k-thinking" : "";
    const h = 22 + detHash(bi * 7 + k.length); // stable per position+kind
    return `<i class="${cls}" style="height:${h}px"></i>`;
  }).join("");
}

function renderDrawerLog() {
  const log = $("drawerLog");
  log.innerHTML = "";
  // Local pairing map: the drawer renders its own transcript through the same
  // rich renderers as the live console without touching its open-tools state.
  const localTools = new Map();
  const evs = (drawerEvents || []);
  for (const e of evs) {
    if (!matchesFilter(e, drawerFilter)) continue;
    log.appendChild(buildEventNode(e, localTools));
  }
  log.scrollTop = log.scrollHeight;
}

let drawerEvents = [];

async function openDrawer(runId) {
  drawerRunId = runId;
  const run = await api("GET", "/api/v1/runs/" + runId);
  const evs = (await api("GET", "/api/v1/runs/" + runId + "/events")).events || [];
  drawerEvents = evs;
  const botName = {};
  for (const b of roster.bots) botName[b.id] = b.name;
  $("drawerBot").textContent = botName[run.bot_id] || run.bot_id || "—";
  $("drawerTitle").textContent = run.id.slice(0, 8) + " · " + (run.status || "");
  const meta = [
    ["status", run.status],
    ["intent", run.intent],
    ["bot", botName[run.bot_id] || run.bot_id || "—"],
    ["route", run.route || run.pin || "—"],
    ["yolo", run.yolo ? "on" : "off"],
    ["verify", run.verify ? "on" : "off"],
    ["events", String(evs.length)],
    ["created", (run.created_at || "").replace("T", " ").slice(0, 19)]
  ];
  $("drawerMeta").innerHTML = meta.map(([k, v]) =>
    `<div class="meta"><div class="mk">${esc(k)}</div><div class="mv">${esc(v)}</div></div>`).join("");
  $("drawerPrompt").textContent = run.prompt || "";
  sparkline(evs);
  renderDrawerLog();
  // Review-diff button: opens the Editor panel with this run's diff.
  const rb = $("drawerReview");
  if (rb) {
    rb.hidden = false;
    rb.onclick = () => {
      closeDrawer();
      gotoPanel("editor");
      if (window.codeStudio) window.codeStudio.openRunDiff(runId);
    };
  }
  $("drawer").classList.add("open");
  $("drawer").setAttribute("aria-hidden", "false");
}

function closeDrawer() {
  $("drawer").classList.remove("open");
  $("drawer").setAttribute("aria-hidden", "true");
}

$("drawerClose").onclick = closeDrawer;
$("drawerScrim").onclick = closeDrawer;
$("drawerStream").onclick = () => {
  closeDrawer();
  clearLog(); stopPoll(); activeRun = null;
  gotoPanel("run");
  startStream(drawerRunId);
};
$("drawerCopy").onclick = async () => {
  const text = drawerEvents.map(e => {
    const t = e.text ? " " + sanitize(e.text) : "";
    return `[${e.kind}]${t}`;
  }).join("\n");
  try {
    await navigator.clipboard.writeText(text);
    toast("transcript copied", "ok");
  } catch { toast("copy failed", "err"); }
};

// ---- command palette -----------------------------------------------------

const paletteItems = () => [
  {ico:"◎", txt:"Overview", kbd:"1", go:() => gotoPanel("overview")},
  {ico:"◍", txt:"Teammates", kbd:"2", go:() => gotoPanel("bots")},
  {ico:"▶", txt:"New run", kbd:"r", go:() => gotoPanel("run")},
  {ico:"❯", txt:"Editor", kbd:"e", go:() => gotoPanel("editor")},
  {ico:"↻", txt:"Routines", kbd:"4", go:() => gotoPanel("routines")},
  {ico:"✦", txt:"Skills", kbd:"5", go:() => gotoPanel("skills")},
  {ico:"⇄", txt:"Routes", kbd:"6", go:() => gotoPanel("routes")},
  {ico:"♥", txt:"Model health", kbd:"7", go:() => gotoPanel("health")},
  {ico:"⟳", txt:"Probe routes", kbd:"", go:() => { gotoPanel("health"); probeRoutes(); }},
  {ico:"⌘", txt:"Refresh mesh models", kbd:"", go:() => { loadMesh().then(refresh); }},
  {ico:"◆", txt:"Refresh direct models", kbd:"", go:() => { loadDirect().then(refresh); }},
  {ico:"◍", txt:"New teammate", kbd:"", go:() => { gotoPanel("bots"); $("botName").focus(); }},
  {ico:"✦", txt:"New skill", kbd:"", go:() => { gotoPanel("skills"); $("skillName").focus(); }},
];

let paletteSel = 0;
function openPalette() {
  paletteSel = 0;
  $("paletteInput").value = "";
  renderPalette("");
  $("palette").classList.add("open");
  $("palette").setAttribute("aria-hidden", "false");
  $("paletteInput").focus();
}
function closePalette() {
  $("palette").classList.remove("open");
  $("palette").setAttribute("aria-hidden", "true");
}
function renderPalette(q) {
  const list = $("paletteList");
  const items = paletteItems().filter(it =>
    !q || it.txt.toLowerCase().includes(q.toLowerCase()));
  list.innerHTML = items.map((it, i) =>
    `<div class="palette-item${i === paletteSel ? " sel" : ""}" data-i="${i}">
      <span class="ico">${it.ico}</span><span class="txt">${it.txt}</span><span class="kbd">${it.kbd}</span>
    </div>`).join("");
  list.querySelectorAll(".palette-item").forEach(el => el.onclick = () => {
    const it = items[+el.dataset.i];
    closePalette();
    it.go();
  });
}
$("paletteInput").oninput = e => { paletteSel = 0; renderPalette(e.target.value); };
$("paletteInput").onkeydown = e => {
  const items = paletteItems().filter(it =>
    !e.target.value || it.txt.toLowerCase().includes(e.target.value.toLowerCase()));
  if (e.key === "ArrowDown") { e.preventDefault(); paletteSel = (paletteSel + 1) % Math.max(items.length,1); renderPalette(e.target.value); }
  else if (e.key === "ArrowUp") { e.preventDefault(); paletteSel = (paletteSel - 1 + Math.max(items.length,1)) % Math.max(items.length,1); renderPalette(e.target.value); }
  else if (e.key === "Enter") { e.preventDefault(); const it = items[paletteSel]; if (it) { closePalette(); it.go(); } }
};
$("paletteScrim").onclick = closePalette;

// ---- keyboard shortcuts --------------------------------------------------

document.addEventListener("keydown", e => {
  const typing = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName || "");
  // §7.3 global keyboard map. Cmd-P / Cmd-. / Cmd-Shift-R / Cmd-Shift-Z
  // work even while typing (they are not text keys), so handle them before
  // the typing guard.
  const mod = e.metaKey || e.ctrlKey;
  if (mod && e.key.toLowerCase() === "p") {
    // File quick-open (Code Studio). Opens the editor panel and its
    // palette; Cmd-P again closes it.
    e.preventDefault();
    if (window.codeStudioInit) window.codeStudioInit();
    gotoPanel("editor");
    if (window.codeStudio && window.codeStudio.openQuick) window.codeStudio.openQuick();
    return;
  }
  if (mod && e.key === ".") {
    // Stop the active run.
    e.preventDefault();
    stopRun().catch(e => showErr(e.message));
    return;
  }
  if (mod && e.shiftKey && e.key.toLowerCase() === "r") {
    // Open the review for the latest run that has a diff.
    e.preventDefault();
    openLatestReview();
    return;
  }
  if (mod && e.shiftKey && e.key.toLowerCase() === "z") {
    // Restore the latest checkpoint (one-keystroke undo of a run).
    e.preventDefault();
    restoreLatestCheckpoint();
    return;
  }
  if (mod && e.key.toLowerCase() === "k") {
    e.preventDefault();
    $("palette").classList.contains("open") ? closePalette() : openPalette();
    return;
  }
  if (e.key === "Escape") {
    if ($("palette").classList.contains("open")) closePalette();
    else if ($("drawer").classList.contains("open")) closeDrawer();
    return;
  }
  if (typing) return;
  if (e.key === "r" || e.key === "R") { gotoPanel("run"); return; }
  const n = parseInt(e.key, 10);
  if (n >= 1 && n <= 7) {
    const names = ["overview","bots","run","routines","skills","routes","health"];
    gotoPanel(names[n-1]);
  }
});

// openLatestReview jumps to the Editor panel with the diff of the most
// recent run that has one. The roster runs are newest-first; the first
// with a diffSummary is the one to review. Falls back to the newest run
// regardless so Cmd-Shift-R always does something visible.
function openLatestReview() {
  const runs = roster.runs || [];
  let target = runs.find(r => r.diffSummary) || runs[0];
  if (!target) { toast("no runs yet", "err"); return; }
  if (window.codeStudioInit) window.codeStudioInit();
  gotoPanel("editor");
  if (window.codeStudio) window.codeStudio.openRunDiff(target.id);
}

// restoreLatestCheckpoint restores the newest checkpoint, with a confirm
// (it rewrites the workspace). Mirrors the per-checkpoint restore button
// in the Editor panel; AC-2d's "one keystroke from anywhere".
async function restoreLatestCheckpoint() {
  let cps;
  try {
    const r = await api("GET", "/api/v1/checkpoints?limit=1");
    cps = r.checkpoints || [];
  } catch (e) { showErr(e.message); return; }
  if (!cps.length) { toast("no checkpoints yet", "err"); return; }
  const cp = cps[0];
  if (!confirm("restore checkpoint " + cp.id + "? current state is saved as a safety checkpoint")) return;
  try {
    const r2 = await api("POST", "/api/v1/checkpoints/" + encodeURIComponent(cp.id) + "/restore", {});
    toast("restored (safety: " + r2.safetyCheckpoint + ")");
    if (window.codeStudio) window.codeStudio.refresh();
  } catch (e) { showErr(e.message); }
}

// ---- auto model-health ---------------------------------------------------

async function probeRoutes() {
  try {
    const h = await api("GET", "/api/v1/health/models");
    const probes = h.probes || [];
    const okN = probes.filter(p => p.ok).length;
    const el = $("routeHealth");
    if (!el) return;
    if (!probes.length) { el.innerHTML = `<span class="led warn"></span>routes — no probes`; return; }
    const allOk = okN === probes.length;
    el.innerHTML = `<span class="led ${allOk ? "ok" : "err"}"></span>routes ${okN}/${probes.length} up`;
    // also fill the health table if it exists
    const tb = $("health")?.querySelector("tbody");
    if (tb) tb.innerHTML = probes.map(p => {
      const mm = p.qwen_mismatch ? " qwen_mismatch" : "";
      return `<tr><td>${esc(p.id)}</td><td>${p.ok ? `<span class="chip ok">ok</span>` : `<span class="chip err">fail</span>`}</td><td>${esc((p.ids||[]).join(", "))}</td><td class="tag">${esc(p.error||"")}${mm}</td></tr>`;
    }).join("");
  } catch (e) {
    const el = $("routeHealth");
    if (el) el.innerHTML = `<span class="led err"></span>routes — ${esc(e.message || "probe failed")}`;
  }
}

// ---- actions ------------------------------------------------------------

// withBusy disables a button while its async action is in flight, so a
// slow request can't be double-submitted and the UI shows progress.
function withBusy(btn, fn) {
  return async () => {
    if (btn.disabled) return;
    btn.disabled = true;
    try { await fn(); }
    finally { btn.disabled = false; }
  };
}
$("createBot").onclick = withBusy($("createBot"), async () => {
  try {
    await api("POST", "/api/v1/bots", {name:$("botName").value, role:$("botRole").value, route:$("botRoute").value});
    toast("teammate added", "ok");
    await refresh();
  } catch (e) { showErr(e.message); }
});
$("memBtn").onclick = withBusy($("memBtn"), async () => {
  try {
    await api("POST", "/api/v1/bots/"+$("memBot").value+"/remember", {note:$("memNote").value});
    $("memNote").value = "";
    toast("remembered", "ok");
    await refresh();
  } catch (e) { showErr(e.message); }
});
$("createSkill").onclick = withBusy($("createSkill"), async () => {
  try {
    await api("POST", "/api/v1/skills", {name:$("skillName").value, body:$("skillBody").value});
    toast("skill added", "ok");
    await refresh();
  } catch (e) { showErr(e.message); }
});
$("createRtn").onclick = withBusy($("createRtn"), async () => {
  try {
    await api("POST", "/api/v1/routines", {bot_id:$("rtnBot").value, skill_id:$("rtnSkill").value, trigger:$("rtnTrigger").value});
    toast("routine scheduled", "ok");
    await refresh();
  } catch (e) { showErr(e.message); }
});
$("sendHandoff").onclick = withBusy($("sendHandoff"), async () => {
  try {
    const out = await api("POST", "/api/v1/handoff", {from:$("fromBot").value, to:$("toBot").value, body:$("handoffBody").value, run:$("handoffRun").checked});
    toast("handoff sent", "ok");
    await refresh();
    if (out.run && out.run.id) { clearLog(); stopPoll(); activeRun = null; gotoPanel("run"); startStream(out.run.id); }
  } catch (e) { showErr(e.message); }
});
$("runBtn").onclick = withBusy($("runBtn"), async () => {
  try {
    const run = await api("POST", "/api/v1/runs", {
      bot_id: $("runBot").value,
      prompt: $("prompt").value,
      intent: $("intent").value,
      yolo: $("yolo").checked,
      verify: $("verify").checked,
      worktree: $("worktree").value
    });
    clearLog();
    stopPoll();
    activeRun = null;
    gotoPanel("run");
    startStream(run.id);
  } catch (e) { showErr(e.message); }
});
// \u2318\u21a9 sends the run prompt without leaving the keyboard.
$("prompt").onkeydown = e => {
  if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
    e.preventDefault();
    $("runBtn").click();
  }
};
// Overview quick run: one prompt + one click. Defaults mirror the Run
// panel (intent=code, no yolo/verify) so the easiest use case is trivial.
$("qRun").onclick = withBusy($("qRun"), async () => {
  const prompt = $("qPrompt").value.trim();
  if (!prompt) { $("qPrompt").focus(); return; }
  try {
    const run = await api("POST", "/api/v1/runs", {
      bot_id: $("qBot").value,
      prompt,
      intent: "code"
    });
    $("qPrompt").value = "";
    clearLog();
    stopPoll();
    activeRun = null;
    gotoPanel("run");
    startStream(run.id);
  } catch (e) { showErr(e.message); }
});
$("qPrompt").onkeydown = e => {
  if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
    e.preventDefault();
    $("qRun").click();
  }
};
// Overview quick remember: teach a teammate without leaving home.
$("qMemBtn").onclick = withBusy($("qMemBtn"), async () => {
  const note = $("qMemNote").value.trim();
  if (!note) { $("qMemNote").focus(); return; }
  try {
    await api("POST", "/api/v1/bots/"+$("qMemBot").value+"/remember", {note});
    $("qMemNote").value = "";
    toast("remembered", "ok");
    await refresh();
  } catch (e) { showErr(e.message); }
});
$("qMemNote").onkeydown = e => {
  if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
    e.preventDefault();
    $("qMemBtn").click();
  }
};
$("stopBtn").onclick = () => { stopRun().catch(e => showErr(e.message)); };
async function testRun(id) {
  try {
    const rt = await api("POST", "/api/v1/routines/"+id+"/test-run");
    await refresh();
    if (rt.run_id) { clearLog(); stopPoll(); activeRun = null; gotoPanel("run"); startStream(rt.run_id); }
  } catch (e) { showErr(e.message); }
}
async function enableRtn(id) {
  try {
    await api("POST", "/api/v1/routines/"+id+"/enable");
    toast("routine enabled", "ok");
    await refresh();
  } catch (e) { showErr(e.message); }
}
async function pauseRtn(id, paused) {
  try {
    await api("POST", "/api/v1/routines/"+id+"/pause", { paused });
    toast(paused ? "routine paused" : "routine resumed", "ok");
    await refresh();
  } catch (e) { showErr(e.message); }
}
async function delRtn(id) {
  try {
    await api("DELETE", "/api/v1/routines/"+id);
    toast("routine deleted", "ok");
    await refresh();
  } catch (e) { showErr(e.message); }
}
async function delBot(id) {
  try {
    await api("DELETE", "/api/v1/bots/"+id);
    toast("bot deleted", "ok");
    await refresh();
  } catch (e) { showErr(e.message); }
}
async function delSkill(id) {
  try {
    await api("DELETE", "/api/v1/skills/"+id);
    toast("skill deleted", "ok");
    await refresh();
  } catch (e) { showErr(e.message); }
}
// relTime renders a short relative timestamp ("2m ago"); blank on parse
// failure so the row degrades gracefully instead of showing "Invalid Date".
function relTime(iso) {
  if (!iso) return "";
  const t = Date.parse(iso);
  if (isNaN(t)) return "";
  const s = Math.max(0, (Date.now() - t) / 1000);
  if (s < 60) return Math.floor(s) + "s ago";
  if (s < 3600) return Math.floor(s / 60) + "m ago";
  if (s < 86400) return Math.floor(s / 3600) + "h ago";
  return Math.floor(s / 86400) + "d ago";
}
// runDuration renders a run's wall time ("12s", "4m03s"); empty while
// running or when timestamps are missing/unparseable.
function runDuration(r) {
  if (!r || !r.created_at) return "";
  const start = Date.parse(r.created_at);
  if (isNaN(start)) return "";
  const end = r.ended_at ? Date.parse(r.ended_at) : Date.now();
  if (isNaN(end)) return "";
  const s = Math.max(0, Math.floor((end - start) / 1000));
  if (s < 60) return s + "s";
  const m = Math.floor(s / 60);
  return m + "m" + String(s % 60).padStart(2, "0") + "s";
}
$("probeBtn").onclick = async () => {
  try {
    const h = await api("GET", "/api/v1/health/models");
    const tb = $("health").querySelector("tbody");
    tb.innerHTML = (h.probes||[]).map(p => {
      const mm = p.qwen_mismatch ? " qwen_mismatch" : "";
      return `<tr><td>${esc(p.id)}</td><td>${p.ok ? `<span class="chip ok">ok</span>` : `<span class="chip err">fail</span>`}</td><td>${esc((p.ids||[]).join(", "))}</td><td class="tag">${esc(p.error||"")}${mm}</td></tr>`;
    }).join("");
  } catch (e) { showErr(e.message); }
};

// ---- init ---------------------------------------------------------------

showSkeletons();
refresh().catch(e => showErr(e.message));
loadMesh().catch(e => showErr(e.message));
loadDirect().catch(e => showErr(e.message));
probeRoutes();
startStream(null);
const meshRefreshBtn = $("meshRefresh");
if (meshRefreshBtn) meshRefreshBtn.onclick = () => loadMesh().catch(e => showErr(e.message));
const directRefreshBtn = $("directRefresh");
if (directRefreshBtn) directRefreshBtn.onclick = () => loadDirect().catch(e => showErr(e.message));

// ---- Code Studio exports (editor.js) --------------------------------------
// editor.js is a separate script; expose the shared helpers it needs.
window.api = api;
window.toast = toast;
window.showErr = showErr;
window.esc = esc;

// resultContractEl renders the taskclient result contract as chips:
// confidence, tests, and a requires_human_review banner when set.
function resultContractEl(payload) {
  let c = {};
  try { c = JSON.parse(payload || "{}"); } catch {}
  const d = document.createElement("div");
  d.className = "ev result-contract";
  const chips = document.createElement("span");
  chips.className = "rc-chips";
  const conf = document.createElement("span");
  conf.className = "rc-chip";
  const pct = Math.round((c.confidence || 0) * 100);
  conf.textContent = "confidence " + pct + "%";
  conf.classList.add(pct >= 70 ? "ok" : pct >= 40 ? "mid" : "low");
  chips.appendChild(conf);
  if (c.tests_total) {
    const t = document.createElement("span");
    t.className = "rc-chip " + (c.tests_passed === c.tests_total ? "ok" : "low");
    t.textContent = "tests " + c.tests_passed + "/" + c.tests_total;
    chips.appendChild(t);
  }
  d.appendChild(chips);
  if (c.summary) {
    const s = document.createElement("div");
    s.className = "rc-summary";
    s.textContent = c.summary;
    d.appendChild(s);
  }
  if (c.patch_sha256) {
    const p = document.createElement("span");
    p.className = "rc-chip";
    p.title = "patch sha256 " + c.patch_sha256;
    p.textContent = "patch " + String(c.patch_sha256).slice(0, 8) +
      (c.patch_changed_files ? " · " + c.patch_changed_files + " files" : "");
    d.appendChild(p);
  }
  if (c.requires_human_review) {
    const b = document.createElement("span");
    b.className = "rc-banner";
    b.textContent = "requires human review";
    d.appendChild(b);
  }
  return d;
}

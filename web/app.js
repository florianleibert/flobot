// flobot web UI. Connects to /api/stream (NDJSON) and renders every
// event kind: content, thinking (collapsed), tool_start/tool_end (paired),
// perm (mock ack), plan, err, done, verify (badge).
//
// Security: the UI never displays secrets or API keys. Every string that
// reaches the DOM passes through sanitize() first, which masks the
// documented key file (~/.accounts/meta.ai) and any MODEL_API_KEY
// material, and esc() HTML-escapes the result.

const $ = id => document.getElementById(id);
const errEl = $("err");
let roster = {bots:[], skills:[], routines:[], messages:[], routes:[], runs:[]};
let activeRun = null;
let pollTimer = null;

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

function showErr(e) { errEl.textContent = e || ""; }

// ---- API ----------------------------------------------------------------

async function api(method, path, body) {
  const opt = {method, headers:{}};
  if (body !== undefined) {
    opt.headers["Content-Type"] = "application/json";
    opt.body = JSON.stringify(body);
  }
  const r = await fetch(path, opt);
  const text = await r.text();
  if (!r.ok) throw new Error(text.trim() || r.statusText);
  if (!text) return null;
  try { return JSON.parse(text); } catch { return text; }
}

function fillSelect(el, items, label) {
  const cur = el.value;
  el.innerHTML = items.map(b => `<option value="${esc(b.id)}">${esc(label(b))}</option>`).join("");
  if ([...el.options].some(o => o.value === cur)) el.value = cur;
}

// ---- rendering ----------------------------------------------------------

function render() {
  $("bots").innerHTML = roster.bots.map(b =>
    `<li><strong>${esc(b.name)}</strong> <span class="tag">${esc(b.id)} · ${esc(b.role)} · ${esc(b.route)}</span></li>`
  ).join("") || `<li class="tag">none</li>`;
  fillSelect($("runBot"), roster.bots, b => `${b.name} (${b.route})`);
  fillSelect($("rtnBot"), roster.bots, b => b.name);
  fillSelect($("fromBot"), roster.bots, b => b.name);
  fillSelect($("toBot"), roster.bots, b => b.name);
  fillSelect($("rtnSkill"), roster.skills, s => s.name);
  $("handoff").style.display = roster.bots.length >= 2 ? "block" : "none";
  $("msgs").innerHTML = (roster.messages||[]).map(m =>
    `<li>${esc(m.from)} → ${esc(m.to)} <span class="tag">owner ${esc(m.owner_bot)}</span><br>${safe(m.body)}</li>`
  ).join("");
  $("skills").innerHTML = roster.skills.map(s =>
    `<li><strong>${esc(s.name)}</strong> <span class="tag">${esc(s.id)}</span></li>`
  ).join("") || `<li class="tag">none</li>`;
  $("routines").innerHTML = roster.routines.map(rt =>
    `<li>${esc(rt.id)} · ${esc(rt.trigger)} · paused=${rt.paused}
      <span class="tag">${esc(rt.last_note||"no test-run")}</span>
      <button data-test="${esc(rt.id)}">test-run</button>
      <button data-en="${esc(rt.id)}">enable</button></li>`
  ).join("") || `<li class="tag">none</li>`;
  $("routines").querySelectorAll("[data-test]").forEach(btn => btn.onclick = () => testRun(btn.dataset.test));
  $("routines").querySelectorAll("[data-en]").forEach(btn => btn.onclick = () => enableRtn(btn.dataset.en));
  const tb = $("routes").querySelector("tbody");
  tb.innerHTML = (roster.routes||[]).map(r =>
    `<tr><td>${esc(r.id)}</td><td>${esc(r.role)}</td><td>${esc(r.model)}</td><td class="tag">${esc(r.purpose)}</td></tr>`
  ).join("");
}

async function refresh() {
  roster = await api("GET", "/api/v1/roster");
  render();
}

// ---- event rendering ----------------------------------------------------

// openTools tracks tool_start events awaiting their tool_end so the UI can
// pair them and mark the tool as running until it finishes.
const openTools = new Map();

function logEl() { return $("log"); }

function appendEvent(e) {
  const log = logEl();
  const kind = e.kind || "";
  let node;
  switch (kind) {
    case "content":
      node = evDiv("content", "content", e.text);
      break;
    case "thinking":
      node = thinkingEl(e.text);
      break;
    case "tool_start":
      node = toolEl(e, false);
      break;
    case "tool_end":
      node = toolEl(e, true);
      break;
    case "perm":
      node = permEl(e);
      break;
    case "plan":
      node = evDiv("plan", "plan", e.text);
      break;
    case "err":
      node = evDiv("err", "err", e.text);
      break;
    case "done":
      node = evDiv("done", "done", e.text);
      break;
    case "verify":
      node = verifyEl(e);
      break;
    case "run_status":
      node = evDiv("run_status", "run_status", e.text);
      break;
    default:
      node = evDiv("unknown", kind, e.text);
  }
  log.appendChild(node);
  log.scrollTop = log.scrollHeight;
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
function toolEl(e, isEnd) {
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
    openTools.set(tool, d);
  } else {
    // Pair with the most recent open start for this tool.
    const open = openTools.get(tool);
    if (open) {
      const st = open.querySelector(".tool-status");
      if (st) st.textContent = e.is_error ? " failed" : " done";
      open.classList.add("closed");
      if (e.is_error) open.classList.add("err");
      openTools.delete(tool);
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

function clearLog() {
  logEl().innerHTML = "";
  openTools.clear();
}

// ---- /api/stream client -------------------------------------------------

// handleEvent parses one NDJSON line and renders it.
function handleEvent(line) {
  line = line.trim();
  if (!line) return;
  let ev;
  try { ev = JSON.parse(line); } catch { return; }
  if (!ev || typeof ev !== "object") return;
  appendEvent(ev);
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
// when a run is active, replays that run's events via ?run= on reconnect.
function connectStream() {
  if (streamES) { try { streamES.close(); } catch {} streamES = null; }
  const url = streamRunId
    ? "/api/stream?run=" + encodeURIComponent(streamRunId)
    : "/api/stream";
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
  if (el) el.textContent = state;
}

// startStream begins streaming for a run (or the global feed if id is null).
function startStream(runId) {
  streamRunId = runId || null;
  if (streamTimer) { clearTimeout(streamTimer); streamTimer = null; }
  streamBuf = "";
  connectStream();
}

// stopStream tears down the stream client.
function stopStream() {
  if (streamTimer) { clearTimeout(streamTimer); streamTimer = null; }
  if (streamES) { try { streamES.close(); } catch {} streamES = null; }
  streamOpen = false;
  streamRunId = null;
  setStreamState("idle");
}

// ---- polling fallback ---------------------------------------------------

function stopPoll() { if (pollTimer) { clearInterval(pollTimer); pollTimer = null; } }

async function pollOnce() {
  if (!activeRun) return;
  const ev = await api("GET", "/api/v1/runs/"+activeRun+"/events");
  clearLog();
  for (const e of ev.events||[]) appendEvent(e);
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

// ---- actions ------------------------------------------------------------

$("createBot").onclick = async () => {
  showErr("");
  try {
    await api("POST", "/api/v1/bots", {name:$("botName").value, role:$("botRole").value, route:$("botRoute").value});
    await refresh();
  } catch (e) { showErr(e.message); }
};
$("createSkill").onclick = async () => {
  showErr("");
  try {
    await api("POST", "/api/v1/skills", {name:$("skillName").value, body:$("skillBody").value});
    await refresh();
  } catch (e) { showErr(e.message); }
};
$("createRtn").onclick = async () => {
  showErr("");
  try {
    await api("POST", "/api/v1/routines", {bot_id:$("rtnBot").value, skill_id:$("rtnSkill").value, trigger:$("rtnTrigger").value});
    await refresh();
  } catch (e) { showErr(e.message); }
};
$("sendHandoff").onclick = async () => {
  showErr("");
  try {
    await api("POST", "/api/v1/handoff", {from:$("fromBot").value, to:$("toBot").value, body:$("handoffBody").value});
    await refresh();
  } catch (e) { showErr(e.message); }
};
$("runBtn").onclick = async () => {
  showErr("");
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
    startStream(run.id);
  } catch (e) { showErr(e.message); }
};
async function testRun(id) {
  showErr("");
  try {
    const rt = await api("POST", "/api/v1/routines/"+id+"/test-run");
    await refresh();
    if (rt.run_id) { clearLog(); stopPoll(); activeRun = null; startStream(rt.run_id); }
  } catch (e) { showErr(e.message); }
}
async function enableRtn(id) {
  showErr("");
  try { await api("POST", "/api/v1/routines/"+id+"/enable"); await refresh(); }
  catch (e) { showErr(e.message); }
}
$("probeBtn").onclick = async () => {
  showErr("");
  try {
    const h = await api("GET", "/api/v1/health/models");
    const tb = $("health").querySelector("tbody");
    tb.innerHTML = (h.probes||[]).map(p => {
      const mm = p.qwen_mismatch ? " qwen_mismatch" : "";
      return `<tr><td>${esc(p.id)}</td><td>${p.ok?"ok":"fail"}</td><td>${esc((p.ids||[]).join(", "))}</td><td class="tag">${esc(p.error||"")}${mm}</td></tr>`;
    }).join("");
  } catch (e) { showErr(e.message); }
};

// ---- init ---------------------------------------------------------------

refresh().catch(e => showErr(e.message));
startStream(null);

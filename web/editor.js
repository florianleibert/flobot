// Code Studio: workspace tree, file editor, quick-open, and run diff
// review. Loaded by app.js after its own definitions; uses window.api,
// window.toast, window.showErr, window.esc from app.js.
(function () {
  "use strict";

  // ---- state ---------------------------------------------------------------
  const S = {
    root: null,          // workspace root path
    name: "",
    openPath: null,       // currently open file path
    openSHA: null,        // sha256 at load time (optimistic concurrency)
    dirty: false,
    expanded: new Set(),  // expanded tree dirs
    quickIndex: [],       // quick-open entries {path, name}
    review: null,         // current run diff {runId, files, totals, ...}
  };

  const $ = id => document.getElementById(id);
  const esc = window.esc || (s => String(s ?? "")
    .replace(/&/g, "&amp;").replace(/</g, "&lt;")
    .replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&#39;"));

  // ---- tree ----------------------------------------------------------------
  async function loadRoot() {
    try {
      const r = await window.api("GET", "/api/v1/workspace/root");
      S.root = r.root; S.name = r.name;
      const el = $("edRoot");
      if (el) el.textContent = S.name;
    } catch (e) {
      const el = $("edRoot");
      if (el) el.textContent = "no workspace (start with --workspace DIR)";
    }
  }

  async function toggleDir(path, btn) {
    if (S.expanded.has(path)) {
      S.expanded.delete(path);
    } else {
      S.expanded.add(path);
    }
    await renderTree();
  }

  async function renderTree() {
    const host = $("edTree");
    if (!host) return;
    // Render each expanded level lazily: root first.
    const frag = document.createDocumentFragment();
    await renderLevel(frag, ".", 0);
    host.replaceChildren(frag);
  }

  async function renderLevel(host, dir, depth) {
    let entries;
    let edStagger = 0;
    try {
      const r = await window.api("GET", "/api/v1/workspace/tree?dir=" + encodeURIComponent(dir));
      entries = r.entries || [];
    } catch (e) {
      const div = document.createElement("div");
      div.className = "ed-node err";
      div.textContent = "tree unavailable";
      host.appendChild(div);
      return;
    }
    for (const e of entries) {
      const node = document.createElement("div");
      node.className = "ed-node" + (e.type === "dir" ? " dir" : "");
      node.style.paddingLeft = (8 + depth * 14) + "px";
      // stagger the fade-in cascade (capped by CSS animation-delay math)
      node.style.setProperty("--ed-i", String(edStagger++ % 24));
      node.style.animationDelay = `calc(var(--ed-i, 0) * 22ms)`;
      const ico = document.createElement("span");
      ico.className = "ed-ico";
      ico.textContent = e.type === "dir" ? (S.expanded.has(e.path) ? "▾" : "▸") : "◦";
      const label = document.createElement("span");
      label.className = "ed-label";
      label.textContent = e.name;
      if (e.type === "dir") label.textContent += "/";
      node.appendChild(ico); node.appendChild(label);
      if (e.type === "file") {
        node.title = e.path;
        node.onclick = () => openFile(e.path);
        if (e.path === S.openPath) node.classList.add("active");
      } else {
        node.onclick = () => toggleDir(e.path);
      }
      host.appendChild(node);
      if (e.type === "dir" && S.expanded.has(e.path)) {
        await renderLevel(host, e.path, depth + 1);
      }
    }
  }

  // ---- quick-open index ------------------------------------------------------
  async function buildQuickIndex() {
    // BFS over the tree API, bounded.
    const out = [];
    const queue = ["."];
    let visits = 0;
    while (queue.length && visits < 40) {
      const dir = queue.shift();
      visits++;
      let r;
      try {
        r = await window.api("GET", "/api/v1/workspace/tree?dir=" + encodeURIComponent(dir));
      } catch { continue; }
      for (const e of r.entries || []) {
        if (e.type === "file") out.push({ path: e.path, name: e.name });
        else if (out.length < 2000) queue.push(e.path);
      }
    }
    S.quickIndex = out;
  }

  function openQuick() {
    const q = $("edQuick");
    if (!q) return;
    q.classList.add("open");
    q.setAttribute("aria-hidden", "false");
    const inp = $("edQuickInput");
    inp.value = "";
    renderQuickList("");
    inp.focus();
    if (S.quickIndex.length === 0) buildQuickIndex().then(() => renderQuickList(inp.value));
  }
  function closeQuick() {
    const q = $("edQuick");
    if (!q) return;
    q.classList.remove("open");
    q.setAttribute("aria-hidden", "true");
  }
  let quickSel = 0;
  function quickMatches(q) {
    q = q.toLowerCase();
    if (!q) return S.quickIndex.slice(0, 12);
    return S.quickIndex.filter(e => e.path.toLowerCase().includes(q)).slice(0, 12);
  }
  function renderQuickList(q) {
    const list = $("edQuickList");
    if (!list) return;
    const items = quickMatches(q);
    if (quickSel >= items.length) quickSel = 0;
    list.innerHTML = items.map((e, i) =>
      `<div class="palette-item${i === quickSel ? " sel" : ""}" data-path="${esc(e.path)}">${esc(e.path)}</div>`).join("");
    list.querySelectorAll(".palette-item").forEach(el => {
      el.onclick = () => { closeQuick(); openFile(el.dataset.path); };
    });
  }

  // ---- editor ----------------------------------------------------------------
  async function openFile(path) {
    if (S.dirty && !confirmDiscard()) return;
    let f;
    try {
      f = await window.api("GET", "/api/v1/workspace/file?path=" + encodeURIComponent(path));
    } catch (e) { window.showErr(e); return; }
    if (!f.exists) { window.toast("file not found: " + path, "err"); return; }
    if (f.binary) { window.toast("binary file (2 MiB cap / NUL sniff)", "err"); return; }
    S.openPath = f.path;
    S.openSHA = f.sha256;
    S.dirty = false;
    const ta = $("edText");
    ta.value = f.content || "";
    // restart the breathe-in animation on each open
    ta.classList.remove("ed-anim");
    void ta.offsetWidth;
    ta.classList.add("ed-anim");
    $("edPath").textContent = f.path;
    $("edStatus").textContent = `${f.size} B`;
    $("edStatus").classList.remove("unsaved");
    ta.disabled = false;
    renderTree();
  }

  // reopenAfterReview reopens the current file after a review mutated
  // the workspace; if the review deleted it, clear the editor instead
  // of leaving a stale buffer a later save would resurrect.
  async function reopenAfterReview() {
    if (!S.openPath) return;
    let f;
    try {
      f = await window.api("GET", "/api/v1/workspace/file?path=" + encodeURIComponent(S.openPath));
    } catch (e) { clearEditor(); return; }
    if (!f.exists) { clearEditor(); return; }
    await openFile(S.openPath);
  }

  function clearEditor() {
    S.openPath = null; S.openSHA = null; S.dirty = false;
    const ta = $("edText");
    if (ta) { ta.value = ""; ta.disabled = true; }
    const p = $("edPath"); if (p) p.textContent = "—";
    const st = $("edStatus"); if (st) st.textContent = "";
  }

  function confirmDiscard() {
    return window.confirm("Discard unsaved changes?");
  }

  async function saveFile() {
    if (!S.openPath) return;
    const ta = $("edText");
    try {
      const f = await window.api("PUT",
        "/api/v1/workspace/file?path=" + encodeURIComponent(S.openPath),
        { content: ta.value, baseSha256: S.openSHA });
      S.openSHA = f.sha256;
      S.dirty = false;
      const st = $("edStatus");
      st.textContent = `saved · ${f.size} B`;
      st.classList.remove("unsaved");
      window.toast("saved " + f.path);
    } catch (e) {
      const msg = String(e.message || e);
      if (msg.includes("409") || msg.includes("changed since read")) {
        window.toast("conflict: file changed on disk — reopen and merge", "err");
      } else {
        window.showErr(e);
      }
    }
  }

  async function newFile() {
    const path = window.prompt("new file path (workspace-relative)");
    if (!path) return;
    try {
      await window.api("POST", "/api/v1/workspace/file?path=" + encodeURIComponent(path), { content: "" });
      await buildQuickIndex();
      await openFile(path);
    } catch (e) { window.showErr(e); }
  }

  async function deleteFile() {
    if (!S.openPath) return;
    if (!window.confirm("delete " + S.openPath + "?")) return;
    try {
      await window.api("DELETE", "/api/v1/workspace/file?path=" + encodeURIComponent(S.openPath));
      S.openPath = null; S.openSHA = null; S.dirty = false;
      $("edText").value = ""; $("edText").disabled = true;
      $("edPath").textContent = "—";
      $("edStatus").textContent = "";
      await buildQuickIndex();
      await renderTree();
    } catch (e) { window.showErr(e); }
  }

  // ---- run diff review ---------------------------------------------------------
  async function loadRunDiff(runId) {
    if (S.reviewBusy) return; // an apply/reject is mid-flight on the old card
    let d;
    try {
      d = await window.api("GET", "/api/v1/runs/" + encodeURIComponent(runId) + "/diff");
    } catch (e) {
      const msg = String(e && e.message || e);
      $("edReview").innerHTML =
        `<div class="ed-review-empty">${esc(msg)}</div>`;
      return;
    }
    S.review = d;
    // A completed review must not render as actionable: the server
    // would 409 on apply/reject. Render the terminal card instead.
    if (d.reviewState && d.reviewState !== "pending") {
      S.review = null;
      const host = $("edReview");
      if (host) host.innerHTML =
        `<div class="ed-review-empty">review complete — ${esc(d.reviewState)}</div>`;
      return;
    }
    renderReview();
  }

  function renderReview() {
    const host = $("edReview");
    const d = S.review;
    if (!d || !d.files || d.files.length === 0) {
      host.innerHTML = `<div class="ed-review-empty">no changes in this run</div>`;
      return;
    }
    const t = d.totals || {};
    let html = `<div class="ed-review-head">
      <span class="ed-diffstat"><b>${esc(t.files || 0)}</b> files <span class="add">+${esc(t.additions || 0)}</span> <span class="del">−${esc(t.deletions || 0)}</span></span>
      <span class="spacer"></span>
      <button type="button" class="ghost" id="edReject">Reject all</button>
      <button type="button" class="primary" id="edApply">Apply selected</button>
    </div>
    <div class="ed-review-keys">j/k move · a/r accept/reject hunk · A accept all · Enter apply · Esc reset</div>`;
    for (const f of d.files) {
      html += `<div class="ed-file" data-path="${esc(f.path)}">
        <div class="ed-file-head">
          <label class="check"><input type="checkbox" class="ed-file-check" checked> <code>${esc(f.path)}</code></label>
          <span class="ed-diffstat sm"><span class="add">+${esc(f.additions)}</span> <span class="del">−${esc(f.deletions)}</span></span>
        </div>`;
      if (f.isBinary || f.isRename) {
        html += `<div class="ed-hunk">${f.isBinary ? "binary file changed" : "renamed from " + esc(f.oldPath || "?")}</div>`;
      } else if (!f.hunks || f.hunks.length === 0) {
        html += `<div class="ed-hunk">whole-file decision (mode-only or empty change)</div>`;
      } else {
        f.hunks.forEach((h, i) => {
          html += `<div class="ed-hunk" data-hunk="${i}">
            <div class="ed-hunk-head"><label class="check"><input type="checkbox" class="ed-hunk-check" checked> <code>${esc(h.header)}</code></label></div>
            <pre class="ed-hunk-body">`;
          for (const ln of h.lines) {
            const cls = ln.type === "add" ? "add" : ln.type === "del" ? "del" : "ctx";
            html += `<span class="ed-line ${cls}">${esc(ln.type === "add" ? "+" : ln.type === "del" ? "−" : " ")}${esc(ln.text)}</span>\n`;
          }
          html += `</pre></div>`;
        });
      }
      html += `</div>`;
    }
    host.innerHTML = html;
    // Re-enable the review buttons (a previous review disabled them).
    const ab = $("edApply"), rb2 = $("edReject");
    if (ab) ab.disabled = false;
    if (rb2) rb2.disabled = false;
    // file checkbox toggles its hunks
    host.querySelectorAll(".ed-file").forEach(fileEl => {
      const fb = fileEl.querySelector(".ed-file-check");
      fb.onchange = () => {
        fileEl.querySelectorAll(".ed-hunk-check").forEach(hb => { hb.checked = fb.checked; });
      };
    });
    $("edApply").onclick = applyReview;
    $("edReject").onclick = rejectReview;
    focusHunk(0);
  }

  // ---- review keyboard navigation (§7.2) --------------------------------
  // j/k move between hunks, a accept focused hunk, r reject focused hunk,
  // A accept all, Enter apply selection, Esc cancel selection edits.
  let reviewFocus = 0;

  function reviewHunks() {
    return Array.from(document.querySelectorAll("#edReview .ed-hunk"));
  }

  function focusHunk(i) {
    const hunks = reviewHunks();
    if (!hunks.length) return;
    if (i < 0) i = hunks.length - 1;
    if (i >= hunks.length) i = 0;
    reviewFocus = i;
    hunks.forEach((h, idx) => h.classList.toggle("focused", idx === i));
    hunks[i].scrollIntoView({ block: "nearest" });
  }

  function toggleFocused(accept) {
    const hunks = reviewHunks();
    const h = hunks[reviewFocus];
    if (!h) return;
    const cb = h.querySelector(".ed-hunk-check");
    if (!cb) return;
    cb.checked = accept;
    // Keep the file checkbox in sync: checked iff all its hunks are.
    const fileEl = h.closest(".ed-file");
    if (!fileEl) return;
    const fb = fileEl.querySelector(".ed-file-check");
    const checks = fileEl.querySelectorAll(".ed-hunk-check");
    if (fb && checks.length) fb.checked = Array.from(checks).every(c => c.checked);
  }

  function acceptAllReview() {
    document.querySelectorAll("#edReview .ed-hunk-check").forEach(cb => { cb.checked = true; });
    document.querySelectorAll("#edReview .ed-file-check").forEach(cb => { cb.checked = true; });
  }

  function cancelReviewSelections() {
    // Esc: reset every selection to its default (all accepted) without
    // touching disk — a non-destructive cancel of partial edits.
    acceptAllReview();
    focusHunk(0);
  }

  function wireReviewKeyboard() {
    document.addEventListener("keydown", e => {
      const panel = $("panel-editor");
      // Only act when the editor panel is actually visible.
      if (!panel || !panel.offsetParent) return;
      if (!S.review) return;
      // Never hijack typing in inputs/textareas or controls.
      const tag = (e.target && e.target.tagName || "").toLowerCase();
      if (tag === "input" || tag === "textarea" || tag === "select" || tag === "button") return;
      const q = $("edQuick");
      if (q && q.classList.contains("open")) return;
      const k = e.key;
      if (k === "j" || k === "J") { e.preventDefault(); focusHunk(reviewFocus + 1); }
      else if (k === "k" || k === "K") { e.preventDefault(); focusHunk(reviewFocus - 1); }
      else if (k === "a") { e.preventDefault(); toggleFocused(true); }
      else if (k === "r") { e.preventDefault(); toggleFocused(false); }
      else if (k === "A") { e.preventDefault(); acceptAllReview(); }
      else if (k === "Enter") { e.preventDefault(); applyReview(); }
      else if (k === "Escape") { e.preventDefault(); cancelReviewSelections(); }
    });
  }

  async function applyReview() {
    const d = S.review;
    if (!d) return;
    if (S.reviewBusy) return; // double-click guard
    S.reviewBusy = true;
    try {
    const selections = [];
    for (const fileEl of $("edReview").querySelectorAll(".ed-file")) {
      const path = fileEl.dataset.path;
      const fb = fileEl.querySelector(".ed-file-check");
      // Binary, renamed, and hunkless (mode-only, empty-file) diffs
      // are whole-file decisions: the file checkbox is the decision
      // (hunks: "all" to keep, unchecked to revert).
      if (fileEl.querySelector(".ed-hunk-body") === null) {
        if (fb && fb.checked) selections.push({ path, hunks: "all" });
        else selections.push({ path, hunks: [] });
        continue;
      }
      const hunks = [];
      fileEl.querySelectorAll(".ed-hunk").forEach(hEl => {
        const cb = hEl.querySelector(".ed-hunk-check");
        if (cb && cb.checked) hunks.push(parseInt(hEl.dataset.hunk, 10));
      });
      selections.push({ path, hunks });
    }
    if (!selections.length) { window.toast("nothing selected", "err"); return; }
    try {
      const r = await window.api("POST",
        "/api/v1/runs/" + encodeURIComponent(d.runId) + "/diff/apply", { selections });
      const skipped = r.skipped || [];
      const reasons = skipped.map(s => `${s.path}: ${s.reason || "skipped"}`).join("\n");
      window.toast(`applied: ${(r.applied || []).length} file(s)` +
        (skipped.length ? ` · skipped: ${skipped.length}` : ""), skipped.length ? "err" : "");
      if (reasons) console.warn("skipped files:\n" + reasons);
      finishReview(r.reviewState || (skipped.length ? "partial" : "accepted"));
      await renderTree();
      await renderCheckpoints();
      await reopenAfterReview();
    } catch (e) { window.showErr(e); }
    } finally { S.reviewBusy = false; }
  }

  // finishReview renders a terminal review card and disables the
  // buttons so a completed review cannot be re-executed.
  function finishReview(state) {
    S.review = null;
    const host = $("edReview");
    if (host) host.innerHTML =
      `<div class="ed-review-empty">review complete — ${esc(state)}</div>`;
    const a = $("edApply"), r = $("edReject");
    if (a) a.disabled = true;
    if (r) r.disabled = true;
  }

  async function rejectReview() {
    const d = S.review;
    if (!d) return;
    if (S.reviewBusy) return; // double-click guard
    if (!window.confirm("reject all changes from this run? The workspace is restored to its pre-run state.")) return;
    S.reviewBusy = true;
    try {
      const r = await window.api("POST", "/api/v1/runs/" + encodeURIComponent(d.runId) + "/diff/reject", {});
      if (r.safetyCheckpoint) {
        window.toast("run rejected; workspace restored (safety checkpoint " + r.safetyCheckpoint + ")");
      } else {
        // No pre-run checkpoint: the server could not revert; the
        // run's changes are still on disk.
        window.toast("run rejected; no pre-run checkpoint — workspace NOT restored", "err");
      }
      finishReview("rejected");
      await renderTree();
      await renderCheckpoints();
      await reopenAfterReview();
    } catch (e) { window.showErr(e); }
    finally { S.reviewBusy = false; }
  }

  // ---- checkpoints ------------------------------------------------------------
  async function renderCheckpoints() {
    const host = $("edCheckpoints");
    if (!host) return;
    let r;
    try {
      r = await window.api("GET", "/api/v1/checkpoints?limit=20");
    } catch (e) {
      host.innerHTML = `<div class="ed-review-empty">checkpoints unavailable</div>`;
      return;
    }
    const cps = r.checkpoints || [];
    if (!cps.length) {
      host.innerHTML = `<div class="ed-review-empty">no checkpoints yet (runs in a git repo create them)</div>`;
      return;
    }
    host.innerHTML = cps.map(cp => `
      <div class="ed-cp">
        <span class="ed-cp-tree">${esc(cp.id)}</span>
        <span class="ed-diffstat sm"><span class="add">+${cp.additions || 0}</span> <span class="del">−${cp.deletions || 0}</span></span>
        <span class="spacer"></span>
        <button type="button" class="ghost" data-cp="${esc(cp.id)}">restore</button>
      </div>`).join("");
    host.querySelectorAll("button[data-cp]").forEach(b => {
      b.onclick = async () => {
        if (!window.confirm("restore checkpoint " + b.dataset.cp + "? current state is saved as a safety checkpoint")) return;
        try {
          const r2 = await window.api("POST", "/api/v1/checkpoints/" + encodeURIComponent(b.dataset.cp) + "/restore", {});
          window.toast("restored (safety: " + r2.safetyCheckpoint + ")");
          await renderTree();
          await renderCheckpoints();
          await reopenAfterReview();
        } catch (e) { window.showErr(e); }
      };
    });
  }

  // ---- wiring -------------------------------------------------------------------
  function wire() {
    const ta = $("edText");
    if (!ta) return; // panel not present
    ta.addEventListener("input", () => {
      S.dirty = true;
      const st = $("edStatus");
      st.textContent = "unsaved";
      st.classList.add("unsaved");
    });
    ta.addEventListener("keydown", e => {
      if ((e.metaKey || e.ctrlKey) && e.key === "s") {
        e.preventDefault();
        saveFile();
      }
    });
    $("edSave").onclick = saveFile;
    $("edNew").onclick = newFile;
    $("edDelete").onclick = deleteFile;
    $("edRefresh").onclick = async () => { await loadRoot(); await renderTree(); await buildQuickIndex(); };
    $("edQuickOpen").onclick = openQuick;
    const q = $("edQuick");
    if (q) {
      $("edQuickScrim").onclick = closeQuick;
      const inp = $("edQuickInput");
      inp.oninput = () => { quickSel = 0; renderQuickList(inp.value); };
      inp.onkeydown = e => {
        const items = quickMatches(inp.value);
        if (e.key === "ArrowDown") { e.preventDefault(); quickSel = (quickSel + 1) % Math.max(items.length, 1); renderQuickList(inp.value); }
        else if (e.key === "ArrowUp") { e.preventDefault(); quickSel = (quickSel - 1 + Math.max(items.length, 1)) % Math.max(items.length, 1); renderQuickList(inp.value); }
        else if (e.key === "Enter") { e.preventDefault(); const it = items[quickSel]; if (it) { closeQuick(); openFile(it.path); } }
        else if (e.key === "Escape") { closeQuick(); }
      };
    }
    // expose for app.js integration
    window.codeStudio = {
      openRunDiff: loadRunDiff,
      refresh: async () => { await loadRoot(); await renderTree(); await renderCheckpoints(); },
      openQuick,
      state: S,
    };
  }

  // init when the panel first becomes visible (app.js calls refresh on nav)
  let inited = false;
  window.codeStudioInit = function () {
    if (inited) return;
    inited = true;
    // Consume a diff_ready that arrived before the panel was opened.
    const pending = window.codeStudioPendingRun;
    if (pending) {
      window.codeStudioPendingRun = null;
      loadRunDiff(pending);
    }
    wire();
    wireReviewKeyboard();
    loadRoot().then(renderTree).then(buildQuickIndex).then(renderCheckpoints);
  };
})();

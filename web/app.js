/* Code Studio redesign — esbuild bundle of src/*.js, embedded via web/embed.go. Edit flobot-ui-redesign/src and run sync-to-flobot.mjs. */
(() => {
  // src/ctx.js
  var $ = (id) => document.getElementById(id);
  var OUTLINE_RULES = {
    go: [
      [/^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)/, "func"],
      [/^\s*type\s+([A-Za-z_]\w*)/, "type"],
      [/^\s*(?:const|var)\s+([A-Za-z_]\w*)/, "const"]
    ],
    js: [
      [/^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][\w$]*)/, "func"],
      [/^\s*(?:export\s+)?(?:abstract\s+)?class\s+([A-Za-z_$][\w$]*)/, "class"],
      [/^\s*(?:export\s+)?(?:interface|type|enum)\s+([A-Za-z_$][\w$]*)/, "type"],
      [/^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?(?:\(|function\b)/, "fn"]
    ],
    py: [
      [/^\s*def\s+([A-Za-z_]\w*)/, "func"],
      [/^\s*class\s+([A-Za-z_]\w*)/, "class"]
    ],
    rs: [
      [/^\s*(?:pub\s+)?(?:async\s+)?(?:unsafe\s+)?fn\s+([A-Za-z_]\w*)/, "func"],
      [/^\s*(?:pub\s+)?(?:struct|enum|trait|impl)\s+([A-Za-z_]\w*)/, "type"]
    ],
    sh: [
      [/^\s*(?:function\s+)?([A-Za-z_]\w*)\s*\(\)\s*\{/, "func"]
    ]
  };
  var EXT_LANG = {
    go: "go",
    js: "js",
    jsx: "js",
    mjs: "js",
    cjs: "js",
    ts: "js",
    tsx: "js",
    py: "py",
    rs: "rs",
    sh: "sh",
    bash: "sh",
    zsh: "sh"
  };
  function langFor(path) {
    const m = /\.([A-Za-z0-9]+)$/.exec(String(path || ""));
    if (!m) return null;
    return EXT_LANG[m[1].toLowerCase()] || null;
  }
  function splitPath(path) {
    return String(path || "").split("/").filter(Boolean);
  }
  function createCtx(api3) {
    const { ed: ed2, edApi: edApi2, safe: safe3, toast: toast4, getQuickIndex, ensureQuickIndex, renderTree: renderTree2, edExpanded: edExpanded2 } = api3;
    function renderBreadcrumbs(path) {
      const host = $("edCrumbs");
      if (!host) return;
      const segs = splitPath(path);
      host.replaceChildren();
      if (!segs.length) return;
      segs.forEach((seg, i) => {
        const isLast = i === segs.length - 1;
        if (i > 0) {
          const sep = document.createElement("span");
          sep.className = "ed-crumb-sep";
          sep.textContent = "/";
          host.appendChild(sep);
        }
        const b = document.createElement("button");
        b.type = "button";
        b.className = "ed-crumb" + (isLast ? " file" : " dir");
        b.textContent = seg;
        b.title = segs.slice(0, i + 1).join("/");
        if (isLast) {
          b.onclick = () => toast4(safe3(segs.join("/")), "");
        } else {
          const dir = segs.slice(0, i + 1).join("/");
          b.onclick = () => {
            edExpanded2.add(dir);
            renderTree2();
          };
        }
        host.appendChild(b);
      });
    }
    function gotoLine(lineNo) {
      const ta = $("edContent");
      if (!ta) return;
      const lines = ta.value.split("\n");
      const idx = Math.max(0, Math.min(lines.length - 1, lineNo - 1));
      let pos = 0;
      for (let i = 0; i < idx; i++) pos += lines[i].length + 1;
      ta.focus();
      ta.setSelectionRange(pos, pos + lines[idx].length);
      const lineHeight = parseFloat(getComputedStyle(ta).lineHeight) || 20;
      ta.scrollTop = Math.max(0, (idx - 4) * lineHeight);
    }
    function renderOutline(content, path) {
      const host = $("edOutline");
      if (!host) return;
      host.replaceChildren();
      const lang = langFor(path);
      const rules = lang ? OUTLINE_RULES[lang] : null;
      if (!rules) {
        const note = document.createElement("div");
        note.className = "ed-outline-note";
        note.textContent = lang === null ? "no outline for this file type" : "";
        host.appendChild(note);
        return;
      }
      const lines = String(content || "").split("\n");
      const found = [];
      lines.forEach((line, i) => {
        for (const [re, kind] of rules) {
          const m = re.exec(line);
          if (m) {
            found.push({ name: m[1], kind, line: i + 1 });
            break;
          }
        }
      });
      if (!found.length) {
        const note = document.createElement("div");
        note.className = "ed-outline-note";
        note.textContent = "no symbols found";
        host.appendChild(note);
        return;
      }
      for (const s of found) {
        const b = document.createElement("button");
        b.type = "button";
        b.className = "ed-outline-item";
        b.title = s.kind + " \xB7 line " + s.line;
        const k = document.createElement("span");
        k.className = "ed-outline-kind " + s.kind;
        k.textContent = s.kind;
        const n = document.createElement("span");
        n.className = "ed-outline-name";
        n.textContent = s.name;
        b.appendChild(k);
        b.appendChild(n);
        b.onclick = () => gotoLine(s.line);
        host.appendChild(b);
      }
    }
    function mentionToken(input) {
      const upto = input.value.slice(0, input.selectionStart ?? input.value.length);
      const m = /@([\w./\-*]*)$/.exec(upto);
      return m ? m[1] : null;
    }
    function hideMention() {
      const pop = $("edMention");
      if (pop) {
        pop.hidden = true;
        pop.replaceChildren();
      }
    }
    async function showMention(term) {
      const pop = $("edMention");
      if (!pop) return;
      const t = String(term || "").toLowerCase();
      let files = (getQuickIndex() || []).filter((f) => !t || f.path.toLowerCase().includes(t));
      if (!files.length && ensureQuickIndex) {
        await ensureQuickIndex();
        files = (getQuickIndex() || []).filter((f) => !t || f.path.toLowerCase().includes(t));
      }
      pop.replaceChildren();
      if (!files.length) {
        hideMention();
        return;
      }
      for (const f of files.slice(0, 30)) {
        const item = document.createElement("button");
        item.type = "button";
        item.className = "ed-mention-item";
        const ico = document.createElement("span");
        ico.className = "ed-ico";
        ico.textContent = "\u25E6";
        const label = document.createElement("span");
        label.textContent = f.path;
        item.appendChild(ico);
        item.appendChild(label);
        item.onclick = () => {
          applyMention(f.path);
          hideMention();
        };
        pop.appendChild(item);
      }
      pop.hidden = false;
    }
    function applyMention(path) {
      const input = $("edPrompt");
      if (!input) return;
      const caret = input.selectionStart ?? input.value.length;
      const before = input.value.slice(0, caret);
      const after = input.value.slice(caret);
      const at = before.lastIndexOf("@");
      if (at < 0) return;
      const next = before.slice(0, at) + "@" + path + " " + after;
      input.value = next;
      const pos = at + path.length + 2;
      input.setSelectionRange(pos, pos);
      addContext(path);
    }
    function addContext(path) {
      if (!ed2.ctxPaths.includes(path)) ed2.ctxPaths.push(path);
      renderContext();
    }
    function removeContext(path) {
      ed2.ctxPaths = ed2.ctxPaths.filter((p) => p !== path);
      renderContext();
    }
    function clearContext() {
      ed2.ctxPaths = [];
      renderContext();
    }
    function renderContext() {
      const host = $("edCtx");
      if (!host) return;
      host.replaceChildren();
      host.hidden = ed2.ctxPaths.length === 0;
      for (const p of ed2.ctxPaths) {
        const chip = document.createElement("span");
        chip.className = "ed-ctx-chip";
        const label = document.createElement("span");
        label.textContent = p;
        const x = document.createElement("button");
        x.type = "button";
        x.className = "ed-ctx-x";
        x.textContent = "\xD7";
        x.title = "remove reference";
        x.onclick = () => removeContext(p);
        chip.appendChild(label);
        chip.appendChild(x);
        chip.onclick = (ev) => {
          if (ev.target === x) return;
          api3.openFile(p);
        };
        host.appendChild(chip);
      }
    }
    function contextPaths() {
      return ed2.ctxPaths.slice();
    }
    function wireMention() {
      const input = $("edPrompt");
      if (!input) return;
      input.addEventListener("input", () => {
        const term = mentionToken(input);
        if (term === null) hideMention();
        else showMention(term);
      });
      input.addEventListener("blur", () => setTimeout(hideMention, 120));
      input.addEventListener("keydown", (ev) => {
        if (ev.key === "Escape") hideMention();
      });
    }
    function contextBlock() {
      const paths = contextPaths();
      if (!paths.length) return "";
      return "\n\nReferenced files (read these first):\n" + paths.map((p) => "- " + p).join("\n");
    }
    return {
      renderBreadcrumbs,
      renderOutline,
      gotoLine,
      wireMention,
      contextPaths,
      contextBlock,
      clearContext,
      renderContext,
      hideMention
    };
  }

  // src/editor.js
  var $2 = (id) => document.getElementById(id);
  function esc(s) {
    return String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
  }
  function sanitize(s) {
    let out = String(s ?? "");
    for (const re of [/MODEL_API_KEY\s*[=:]\s*\S+/g, /sk-[A-Za-z0-9_\-]{8,}/g, /Bearer\s+[A-Za-z0-9_\-\.]{8,}/g, /~\/\.accounts\/meta\.ai/g, /\/\.accounts\/meta\.ai/g]) {
      out = out.replace(re, "[REDACTED]");
    }
    return out;
  }
  function safe(s) {
    return esc(sanitize(s));
  }
  function toast(msg, cls) {
    const t = document.createElement("div");
    t.className = "toast" + (cls ? " " + cls : "");
    t.textContent = msg;
    document.getElementById("toasts")?.appendChild(t);
    setTimeout(() => t.remove(), 2200);
  }
  var ed = {
    tree: null,
    openPath: null,
    openSha: null,
    // tabs are the open editors: each keeps its own buffer, file handle and
    // dirty state, so switching files never loses an edit.
    tabs: [],
    activeTab: null,
    // descId is the server-side file descriptor for the open file.
    descId: null,
    dirty: false,
    runs: [],
    // ctxPaths are files referenced from the mission prompt (@-mentions).
    ctxPaths: [],
    // diff review state
    review: null,
    // { runId, files, totals, checkpoint, reviewState }
    sel: {},
    // { [path]: { accepted:Set, rejected:Set } }
    focus: null,
    // [path, hunkIndex]
    // liveTimer polls the in-flight diff while a mission is working.
    liveTimer: null,
    // hunkPrints fingerprints each accepted hunk, so a hunk the agent
    // rewrote after the human accepted it is re-opened instead of silently
    // staying accepted.
    hunkPrints: {}
    // { [path]: { [hunkIndex]: fingerprint } }
  };
  var ctx = null;
  function edApi(method, path, body) {
    const opt = { method, headers: {} };
    if (body !== void 0) {
      opt.headers["Content-Type"] = "application/json";
      opt.body = JSON.stringify(body);
    }
    return fetch(path, opt).then(async (r) => {
      const text = await r.text();
      if (!r.ok) {
        let msg = text.trim() || r.statusText;
        try {
          const parsed = JSON.parse(text);
          if (parsed && parsed.error) msg = String(parsed.error);
        } catch {
        }
        throw new Error(msg);
      }
      if (!text) return null;
      try {
        return JSON.parse(text);
      } catch {
        return text;
      }
    });
  }
  var edExpanded = /* @__PURE__ */ new Set();
  var ED_NOISE_DIRS = /* @__PURE__ */ new Set([
    "node_modules",
    "dist",
    "build",
    "out",
    "coverage",
    "webextensions",
    ".next",
    ".nuxt",
    ".vite",
    ".cache",
    ".turbo",
    ".parcel-cache",
    ".pytest_cache",
    ".mypy_cache",
    ".ruff_cache",
    ".tox",
    "venv",
    ".venv",
    "__pycache__",
    "target",
    "vendor",
    "bower_components",
    ".terraform"
  ]);
  var edShowAll = (() => {
    try {
      return localStorage.getItem("edShowAll") === "1";
    } catch {
      return false;
    }
  })();
  function edNoise(e) {
    if (e.type !== "dir") return false;
    const first = String(e.name).split("/")[0];
    return ED_NOISE_DIRS.has(first) || ED_NOISE_DIRS.has(String(e.name));
  }
  var edTreeCount = { files: 0, hidden: 0 };
  async function loadTree() {
    try {
      const root = await edApi("GET", "/api/v1/workspace/root");
      $2("edRoot").textContent = safe(root.root || root.name || "");
    } catch (e) {
      $2("edRoot").textContent = "no workspace";
    }
    await renderTree();
  }
  async function renderTree() {
    const host = $2("edTree");
    if (!host) return;
    edTreeCount = { files: 0, hidden: 0 };
    const frag = document.createDocumentFragment();
    await renderLevel(frag, ".", 0);
    const note = document.createElement("div");
    note.className = "ed-tree-note";
    note.textContent = edTreeCount.hidden ? edTreeCount.files + " files \xB7 " + edTreeCount.hidden + " artifact dirs hidden" : edTreeCount.files + " files";
    frag.appendChild(note);
    host.replaceChildren(frag);
    const t = $2("edShowFiles");
    if (t) t.classList.toggle("on", edShowAll);
  }
  async function renderLevel(host, dir, depth) {
    let entries;
    try {
      const q = "/api/v1/workspace/tree?dir=" + encodeURIComponent(dir) + (edShowAll ? "&noise=1" : "");
      const r = await edApi("GET", q);
      entries = r.entries || [];
      edTreeCount.hidden += Number(r.hidden || 0);
    } catch (e) {
      const div = document.createElement("div");
      div.className = "ed-node err";
      div.textContent = "tree unavailable";
      host.appendChild(div);
      return;
    }
    for (const e of entries) {
      if (e.type === "file") edTreeCount.files++;
      if (!edShowAll && edNoise(e)) {
        edTreeCount.hidden++;
        continue;
      }
      const node = document.createElement("div");
      node.className = "ed-node" + (e.type === "dir" ? " dir" : " file");
      node.style.paddingLeft = 8 + depth * 14 + "px";
      const ico = document.createElement("span");
      ico.className = "ed-ico";
      ico.textContent = e.type === "dir" ? edExpanded.has(e.path) ? "\u25BE" : "\u25B8" : "\u25E6";
      const label = document.createElement("div");
      label.className = "ed-label";
      label.textContent = e.name;
      if (e.type === "dir") label.textContent += "/";
      node.appendChild(ico);
      node.appendChild(label);
      if (e.type === "file") {
        node.title = e.path;
        node.onclick = () => openFile(e.path);
        if (e.path === ed.openPath) node.classList.add("active");
      } else {
        node.onclick = async () => {
          if (edExpanded.has(e.path)) edExpanded.delete(e.path);
          else edExpanded.add(e.path);
          await renderTree();
        };
      }
      host.appendChild(node);
      if (e.type === "dir" && edExpanded.has(e.path)) {
        await renderLevel(host, e.path, depth + 1);
      }
    }
  }
  async function openDescriptor(path) {
    const d = await edApi("POST", "/api/v1/workspace/descriptors", { path });
    return d && d.id ? d.id : null;
  }
  async function closeDescriptor(id) {
    if (!id) return;
    try {
      await edApi("DELETE", "/api/v1/workspace/descriptors/" + id);
    } catch {
    }
  }
  async function openFile(path) {
    path = String(path || "").trim();
    if (!path) {
      toast("type a path first", "err");
      return;
    }
    const existing = ed.tabs.find((t) => t.path === path);
    if (existing) {
      await activateTab(path, true);
      return;
    }
    try {
      const id = await openDescriptor(path);
      if (!id) throw new Error("could not open " + path);
      const f = await edApi("GET", "/api/v1/workspace/descriptors/" + id);
      ed.descId = id;
      ed.openPath = f.path;
      ed.openSha = f.sha256;
      ed.dirty = false;
      $2("edPath").value = f.path;
      $2("edContent").value = f.content || "";
      $2("edDirty").textContent = "";
      ed.tabs.push({ path: f.path, sha: f.sha256, descId: id, content: f.content || "", saved: f.content || "", dirty: false });
      ed.activeTab = f.path;
      markOpen(f.path);
      ctx?.renderBreadcrumbs(f.path);
      ctx?.renderOutline(f.content || "", f.path);
      renderOpenTabs();
      persistTabs();
    } catch (e) {
      ed.descId = null;
      if (/not found|does not exist/i.test(e.message)) {
        if (confirm(path + " does not exist yet \u2014 create it?")) {
          try {
            const f = await edApi("POST", "/api/v1/workspace/file?path=" + encodeURIComponent(path), { content: "" });
            ed.openPath = f.path;
            ed.openSha = f.sha256;
            ed.dirty = false;
            $2("edPath").value = f.path;
            $2("edContent").value = "";
            $2("edDirty").textContent = "";
            try {
              ed.descId = await openDescriptor(f.path);
            } catch {
            }
            ed.tabs.push({ path: f.path, sha: f.sha256, descId: ed.descId, content: "", saved: "", dirty: false });
            ed.activeTab = f.path;
            renderOpenTabs();
            persistTabs();
            await loadTree();
            ctx?.renderBreadcrumbs(f.path);
            ctx?.renderOutline("", f.path);
            toast("created " + f.path, "ok");
          } catch (e2) {
            toast(safe(e2.message), "err");
          }
        }
        return;
      }
      toast(safe(e.message), "err");
    }
  }
  async function saveFile() {
    if (!ed.openPath) {
      if (!$2("edContent").value) {
        toast("nothing to save", "err");
        return;
      }
      const path = $2("edPath").value.trim();
      if (!path) {
        toast("type a path to save to", "err");
        return;
      }
      try {
        const f = await edApi("POST", "/api/v1/workspace/file?path=" + encodeURIComponent(path), { content: $2("edContent").value });
        ed.openPath = f.path;
        ed.openSha = f.sha256;
        ed.dirty = false;
        $2("edDirty").textContent = "";
        try {
          ed.descId = await openDescriptor(f.path);
        } catch {
        }
        ed.tabs.push({ path: f.path, sha: f.sha256, descId: ed.descId, content: "", saved: "", dirty: false });
        ed.activeTab = f.path;
        renderOpenTabs();
        persistTabs();
        await loadTree();
        toast("created " + f.path, "ok");
      } catch (e) {
        toast(safe(e.message), "err");
      }
      return;
    }
    try {
      let f;
      if (ed.descId) {
        f = await edApi("PUT", "/api/v1/workspace/descriptors/" + ed.descId, {
          content: $2("edContent").value
        });
      } else {
        f = await edApi("PUT", "/api/v1/workspace/file?path=" + encodeURIComponent(ed.openPath), {
          content: $2("edContent").value,
          baseSha256: ed.openSha
        });
      }
      ed.openSha = f.sha256;
      ed.dirty = false;
      $2("edDirty").textContent = "";
      const tab = ed.tabs.find((t) => t.path === ed.openPath);
      if (tab) {
        tab.saved = $2("edContent").value;
        tab.content = tab.saved;
        tab.sha = f.sha256;
        tab.dirty = false;
      }
      renderOpenTabs();
      persistTabs();
      toast("saved " + ed.openPath, "ok");
    } catch (e) {
      const msg = String(e.message || "");
      if (/changed since read|sha mismatch/i.test(msg)) {
        toast("this file changed on disk \u2014 reopen it to see the newer version", "err");
        return;
      }
      toast(safe(e.message), "err");
    }
  }
  function snapshotActive() {
    const tab = ed.tabs.find((t) => t.path === ed.activeTab);
    if (!tab) return;
    tab.content = $2("edContent").value;
    tab.dirty = tab.content !== tab.saved;
    ed.dirty = tab.dirty;
  }
  async function activateTab(path, reload) {
    const tab = ed.tabs.find((t) => t.path === path);
    if (!tab) return;
    snapshotActive();
    if (reload && tab.descId) {
      try {
        const f = await edApi("GET", "/api/v1/workspace/descriptors/" + tab.descId);
        tab.content = f.content || "";
        tab.saved = f.content || "";
        tab.sha = f.sha256;
        tab.dirty = false;
      } catch {
      }
    }
    ed.activeTab = tab.path;
    ed.openPath = tab.path;
    ed.openSha = tab.sha;
    ed.descId = tab.descId;
    ed.dirty = tab.dirty;
    $2("edPath").value = tab.path;
    $2("edContent").value = tab.content;
    $2("edDirty").textContent = tab.dirty ? "\u25CF unsaved" : "";
    markOpen(tab.path);
    ctx?.renderBreadcrumbs(tab.path);
    ctx?.renderOutline(tab.content, tab.path);
    renderOpenTabs();
    persistTabs();
  }
  async function closeTab(path) {
    const idx = ed.tabs.findIndex((t) => t.path === path);
    if (idx < 0) return;
    const tab = ed.tabs[idx];
    if (tab.dirty && !confirm("Close " + tab.path + " without saving?")) return;
    if (tab.descId) await closeDescriptor(tab.descId);
    ed.tabs.splice(idx, 1);
    if (ed.activeTab === path) {
      ed.activeTab = null;
      const next = ed.tabs[Math.min(idx, ed.tabs.length - 1)];
      if (next) {
        await activateTab(next.path, false);
        return;
      }
      ed.openPath = null;
      ed.openSha = null;
      ed.descId = null;
      ed.dirty = false;
      $2("edPath").value = "";
      $2("edContent").value = "";
      $2("edDirty").textContent = "";
      ctx?.renderBreadcrumbs("");
      ctx?.renderOutline("", "");
    }
    renderOpenTabs();
    persistTabs();
  }
  function renderOpenTabs() {
    const host = $2("edOpenTabs");
    if (!host) return;
    host.replaceChildren();
    ed.tabs.forEach((tab) => {
      const el2 = document.createElement("div");
      el2.className = "ed-opentab" + (tab.path === ed.activeTab ? " sel" : "");
      const name = document.createElement("button");
      name.type = "button";
      name.className = "ed-opentab-name";
      name.textContent = tab.path.split("/").pop() || tab.path;
      name.title = tab.path;
      name.onclick = () => activateTab(tab.path, false);
      el2.appendChild(name);
      if (tab.dirty) {
        const dot = document.createElement("span");
        dot.className = "ed-opentab-dirty";
        dot.title = "unsaved changes";
        el2.appendChild(dot);
      }
      const x = document.createElement("button");
      x.type = "button";
      x.className = "ed-opentab-close";
      x.textContent = "\xD7";
      x.title = "close " + tab.path;
      x.onclick = (ev) => {
        ev.stopPropagation();
        closeTab(tab.path);
      };
      el2.appendChild(x);
      host.appendChild(el2);
    });
  }
  function persistTabs() {
    try {
      localStorage.setItem("edTabs", JSON.stringify(ed.tabs.map((t) => ({
        path: t.path,
        content: t.content,
        saved: t.saved,
        dirty: t.dirty
      }))));
    } catch {
    }
  }
  async function restoreTabs() {
    let saved = [];
    try {
      saved = JSON.parse(localStorage.getItem("edTabs") || "[]");
    } catch {
      saved = [];
    }
    if (!Array.isArray(saved) || !saved.length) return;
    for (const t of saved.slice(0, 12)) {
      try {
        const id = await openDescriptor(t.path);
        if (!id) continue;
        const f = await edApi("GET", "/api/v1/workspace/descriptors/" + id);
        ed.tabs.push({
          path: f.path,
          sha: f.sha256,
          descId: id,
          content: t.dirty ? t.content : f.content || "",
          saved: f.content || "",
          dirty: !!t.dirty
        });
      } catch {
      }
    }
    if (ed.tabs.length) await activateTab(ed.tabs[0].path, false);
  }
  function markOpen(path) {
    $2("edTree").querySelectorAll(".ed-node.file").forEach((n) => n.classList.remove("active"));
    const el2 = $2("edTree").querySelector(`.ed-node[title="${CSS.escape(path)}"]`);
    if (el2) el2.classList.add("active");
  }
  function renderReviewState() {
    const el2 = $2("edReviewState");
    if (!el2) return;
    const st = ed.review?.reviewState || ed.review?.review_state || "";
    el2.textContent = st ? st : "";
    el2.className = "tag " + st;
    const j = ed.review?.judgeScore;
    const existing = el2.parentElement?.querySelector(".tag.judge");
    if (existing) existing.remove();
    if (typeof j === "number" && j > 0) {
      const jt = document.createElement("span");
      jt.className = "tag judge";
      jt.textContent = Number(j).toFixed(1) + "/10";
      jt.title = ed.review?.judgeRationale || "";
      el2.after(jt);
    }
  }
  function renderDiff() {
    const box = $2("edDiff");
    const files = ed.review?.files || [];
    reconcileHunks(files);
    const badge = $2("edDiffBadge");
    if (badge) {
      const pending = files.reduce((n, fd) => {
        const sel = ed.sel[fd.path] || { accepted: /* @__PURE__ */ new Set(), rejected: /* @__PURE__ */ new Set() };
        return n + (fd.hunks.length - sel.accepted.size);
      }, 0);
      badge.hidden = !pending;
      badge.textContent = pending ? String(pending) : "";
      badge.classList.toggle("warn", pending > 0);
    }
    if (!files.length) {
      box.innerHTML = `<p class="tag">no run diff yet \u2014 run a mission, then review its changes here.</p>`;
      return;
    }
    box.innerHTML = files.map((fd, fi) => {
      const sel = ed.sel[fd.path] || { accepted: /* @__PURE__ */ new Set(), rejected: /* @__PURE__ */ new Set() };
      const fileCls = ["ed-file-diff"];
      if (sel.accepted.size === fd.hunks.length) fileCls.push("all-accepted");
      const hunks = fd.hunks.map((h, hi) => {
        const cls = ["ed-hunk"];
        if (sel.accepted.has(hi)) cls.push("accepted");
        if (sel.rejected.has(hi)) cls.push("rejected");
        if (ed.focus[0] === fd.path && ed.focus[1] === hi) cls.push("focused");
        const hm = /@@\s*-(\d+)(?:,\d+)?\s*\+(\d+)(?:,\d+)?\s*@@/.exec(h.header || "");
        let oldNo = hm ? +hm[1] : 1, newNo = hm ? +hm[2] : 1;
        const body = h.lines.map((l) => {
          const t = l.type === "add" ? "add" : l.type === "del" ? "del" : "ctx";
          let ln = "";
          if (t === "add") {
            ln = String(newNo++);
          } else if (t === "del") {
            ln = String(oldNo++);
          } else {
            ln = String(oldNo++) + ":" + String(newNo++);
          }
          return `<div class="dl ${t}"><span class="dn">${ln.padStart(5)}</span><span class="dt">${safe(l.text)}</span></div>`;
        }).join("");
        return `<div class="${cls.join(" ")}" data-fi="${fi}" data-hi="${hi}">
        <div class="dh"><span class="tag">${safe(h.header)}</span><span class="spacer"></span>
          <button type="button" class="ghost mini" data-act="a">\u2713 accept</button>
          <button type="button" class="ghost mini" data-act="r">\u2717 reject</button></div>
        ${body}
      </div>`;
      }).join("");
      const meta = fd.isNew ? "new" : fd.isDelete ? "deleted" : fd.isRename ? "renamed" : "";
      return `<div class="${fileCls.join(" ")}">
      <div class="ed-file-head"><span class="tag">${safe(fd.path)}</span>
        <span class="tag">+${fd.additions}/\u2212${fd.deletions}${meta ? " \xB7 " + meta : ""}</span></div>
      ${hunks}
    </div>`;
    }).join("");
  }
  function hunkFingerprint(h) {
    return (h.header || "") + "\n" + (h.lines || []).map((l) => (l.type || "") + (l.text || "")).join("\n");
  }
  function reconcileHunks(files) {
    for (const fd of files || []) {
      const sel = ed.sel[fd.path];
      const prints = ed.hunkPrints[fd.path];
      if (!sel || !prints) continue;
      for (const hi of [...sel.accepted]) {
        const h = fd.hunks[hi];
        if (!h) {
          sel.accepted.delete(hi);
          continue;
        }
        if (prints[hi] !== void 0 && prints[hi] !== hunkFingerprint(h)) {
          sel.accepted.delete(hi);
          sel.rejected.delete(hi);
        }
      }
    }
  }
  function toggleHunk(fdPath, hi, v) {
    const sel = ed.sel[fdPath] || (ed.sel[fdPath] = { accepted: /* @__PURE__ */ new Set(), rejected: /* @__PURE__ */ new Set() });
    if (v === true) {
      sel.accepted.add(hi);
      sel.rejected.delete(hi);
      const h = ed.review?.files?.find((f) => f.path === fdPath)?.hunks?.[hi];
      if (h) {
        const prints = ed.hunkPrints[fdPath] || (ed.hunkPrints[fdPath] = {});
        prints[hi] = hunkFingerprint(h);
      }
    } else if (v === false) {
      sel.rejected.add(hi);
      sel.accepted.delete(hi);
    } else {
      sel.accepted.delete(hi);
      sel.rejected.delete(hi);
    }
    renderDiff();
  }
  async function applySelection() {
    if (!ed.review) return;
    const selections = Object.keys(ed.sel).map((path) => {
      const sel = ed.sel[path];
      const idx = [...sel.accepted].sort((a, b) => a - b);
      return { path, hunks: idx.length === (ed.review.files.find((f) => f.path === path)?.hunks.length || 0) ? "all" : idx };
    }).filter((s) => s.hunks !== "all" || s.hunks.length);
    if (!selections.length) {
      toast("nothing selected to apply", "err");
      return;
    }
    const inWorktree = !!ed.review.worktree;
    const endpoint = inWorktree ? `/api/v1/runs/${ed.review.runId}/diff/merge` : `/api/v1/runs/${ed.review.runId}/diff/apply`;
    try {
      const res = await edApi("POST", endpoint, { selections });
      toast(inWorktree ? "merged into your tree \u2014 " + (res.applied || []).length + " file(s)" : "applied \u2014 review state: " + (res.reviewState || "accepted"), "ok");
      ed.sel = {};
      ed.hunkPrints = {};
      await loadLatestDiff();
      await loadTree();
      if (inWorktree) await window.supervision?.refresh?.();
    } catch (e) {
      toast(safe(e.message), "err");
    }
  }
  async function revertRun() {
    if (!ed.review?.runId) {
      toast("no run diff loaded", "err");
      return;
    }
    const id = ed.review.runId;
    if (!confirm("Reject run " + id.slice(0, 8) + " and revert the workspace to its pre-run state?")) return;
    try {
      const res = await edApi("POST", `/api/v1/runs/${id}/diff/reject`);
      toast("reverted \u2014 review state: " + (res.reviewState || "rejected"), "ok");
      ed.sel = {};
      await loadLatestDiff();
      await loadTree();
      await loadRuns();
    } catch (e) {
      toast(safe(e.message), "err");
    }
  }
  async function loadLatestDiff() {
    try {
      const runs = await edApi("GET", "/api/v1/runs");
      const list = runs.runs || runs || [];
      const withDiff = list.filter((r) => r.diffSummary || r.diff_summary);
      if (!withDiff.length) {
        ed.review = null;
        $2("edDiff").innerHTML = `<p class="tag">no run diff yet \u2014 run a mission, then review its changes here.</p>`;
        return;
      }
      const latest = withDiff[0];
      const d = await edApi("GET", "/api/v1/runs/" + latest.id + "/diff");
      d.judgeScore = latest.judgeScore;
      d.judgeRationale = latest.judgeRationale;
      d.worktree = latest.worktree || latest.worktreePath || "";
      ed.review = d;
      ed.sel = {};
      ed.focus = d.files?.length ? [d.files[0].path, 0] : null;
      const diffPaths = (d.files || []).map((f) => f.path);
      for (const p of diffPaths) edTouchedDiff.add(p);
      const merged = [.../* @__PURE__ */ new Set([...diffPaths, ...edTouched])];
      edTouched.length = 0;
      edTouched.push(...merged.slice(0, 24));
      renderTouched();
      renderDiff();
      renderReviewState();
      return d;
    } catch (e) {
      $2("edDiff").innerHTML = `<p class="tag">${safe(e.message)}</p>`;
      return null;
    }
  }
  function runDuration(r) {
    const s = Date.parse(r.created_at || r.createdAt || "");
    const e = Date.parse(r.ended_at || r.EndedAt || "");
    if (isNaN(s) || isNaN(e) || e < s) return null;
    const sec = Math.round((e - s) / 1e3);
    if (sec < 60) return sec + "s";
    if (sec < 3600) return Math.floor(sec / 60) + "m" + (sec % 60 ? " " + sec % 60 + "s" : "");
    return Math.floor(sec / 3600) + "h" + Math.floor(sec % 3600 / 60) + "m";
  }
  async function loadRuns() {
    const box = $2("edRuns");
    if (!box) return;
    try {
      const runs = await edApi("GET", "/api/v1/runs");
      const list = (runs.runs || runs || []).slice(0, 30);
      const badge = $2("edRunsBadge");
      if (badge) {
        const pending = list.filter((r) => !r.reviewState || r.reviewState === "pending").length;
        badge.hidden = !pending;
        badge.textContent = pending ? String(pending) : "";
        badge.classList.toggle("warn", pending > 0);
      }
      if (!list.length) {
        box.innerHTML = `<p class="tag">no runs yet.</p>`;
        return;
      }
      box.innerHTML = list.map((r) => {
        const st = r.reviewState || r.review_state || "";
        const badge2 = st ? `<span class="tag ${st === "accepted" ? "ok" : st === "rejected" ? "err" : ""}">${safe(st)}</span>` : "";
        const ds = r.diffSummary || r.diff_summary;
        const diffTag = ds ? `<span class="tag">+${ds.additions ?? "?"}/\u2212${ds.deletions ?? "?"}</span>` : "";
        let durTag = "";
        const dur = runDuration(r);
        if (dur !== null) durTag = `<span class="tag">${dur}</span>`;
        const j = r.judgeScore;
        const judgeTag = typeof j === "number" && j > 0 ? `<span class="tag judge" title="${safe(r.judgeRationale || "")}">\u2605 ${Number(j).toFixed(1)}</span>` : "";
        const statusChip = r.status === "done" ? `<span class="chip ok">done</span>` : r.status === "error" ? `<span class="chip err">error</span>` : r.status ? `<span class="chip">${safe(r.status)}</span>` : "";
        return `<div class="ed-run-item${r.id === ed.review?.runId ? " sel" : ""}" data-id="${esc(r.id)}">
        <span class="ck-time">${safe((r.createdAt || r.created_at || r.id).toString().slice(0, 19))}</span>
        <span class="ck-note">${safe((r.prompt || r.title || "").slice(0, 60))}</span>
        ${statusChip}${diffTag}${durTag}${judgeTag}${badge2}
      </div>`;
      }).join("");
      box.querySelectorAll(".ed-run-item").forEach((item) => {
        item.onclick = async () => {
          try {
            const d = await edApi("GET", "/api/v1/runs/" + item.dataset.id + "/diff");
            ed.review = d;
            ed.sel = {};
            ed.focus = d.files?.length ? [d.files[0].path, 0] : null;
            renderDiff();
            renderReviewState();
            switchTab("diff");
            loadRuns();
          } catch (e) {
            toast(safe(e.message), "err");
          }
        };
      });
    } catch (e) {
      box.innerHTML = `<p class="tag">${safe(e.message)}</p>`;
    }
  }
  function wireEditorKeys() {
    document.addEventListener("keydown", (e) => {
      const panelVisible = document.getElementById("panel-editor")?.classList.contains("active");
      if (!panelVisible) return;
      const typing = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName || "");
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s") {
        e.preventDefault();
        saveFile();
        return;
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "p") {
        e.preventDefault();
        window.dispatchEvent(new CustomEvent("code-studio:quick-open"));
        return;
      }
      if (typing) return;
      if (!ed.review?.files?.length) return;
      const files = ed.review.files;
      if (e.key === "j") {
        e.preventDefault();
        const [fp, hi] = ed.focus || [files[0].path, 0];
        const fh = files.find((f) => f.path === fp)?.hunks.length || 0;
        if (hi + 1 < fh) ed.focus = [fp, hi + 1];
        else {
          const ni = files.findIndex((f) => f.path === fp) + 1;
          ed.focus = ni < files.length ? [files[ni].path, 0] : [files[0].path, 0];
        }
        renderDiff();
      } else if (e.key === "k") {
        e.preventDefault();
        const [fp, hi] = ed.focus || [files[0].path, 0];
        if (hi > 0) ed.focus = [fp, hi - 1];
        else {
          const pi = files.findIndex((f) => f.path === fp) - 1;
          const p = pi >= 0 ? files[pi] : files[files.length - 1];
          ed.focus = [p.path, (p.hunks.length || 1) - 1];
        }
        renderDiff();
      } else if (e.key === "a") {
        e.preventDefault();
        if (ed.focus) toggleHunk(ed.focus[0], ed.focus[1], true);
      } else if (e.key === "r") {
        e.preventDefault();
        if (ed.focus) toggleHunk(ed.focus[0], ed.focus[1], false);
      } else if (e.key === "A") {
        e.preventDefault();
        files.forEach((f) => f.hunks.forEach((_, hi) => toggleHunk(f.path, hi, true)));
      } else if (e.key === "Enter") {
        e.preventDefault();
        applySelection();
      }
    });
  }
  var edQuickIndex = [];
  var edQuickRoot = null;
  async function buildQuickIndex() {
    const out = [];
    const queue = ["."];
    let visits = 0;
    while (queue.length && visits < 40) {
      const dir = queue.shift();
      visits++;
      let r;
      try {
        r = await edApi("GET", "/api/v1/workspace/tree?dir=" + encodeURIComponent(dir));
      } catch {
        continue;
      }
      for (const e of r.entries || []) {
        if (e.type === "file") out.push({ path: e.path, name: e.name });
        else if (!edNoise(e) && out.length < 2e3) queue.push(e.path);
      }
    }
    edQuickIndex = out;
    edQuickRoot = $2("edRoot") ? $2("edRoot").textContent : null;
  }
  function invalidateQuickIndex() {
    edQuickIndex = [];
  }
  function openQuick() {
    const q = $2("edQuick");
    if (!q) return;
    q.classList.add("open");
    q.setAttribute("aria-hidden", "false");
    const inp = $2("edQuickInput");
    inp.value = "";
    renderQuickList("");
    inp.focus();
    const root = $2("edRoot") ? $2("edRoot").textContent : null;
    if (edQuickIndex.length === 0 || edQuickRoot !== root) buildQuickIndex().then(() => renderQuickList(inp.value));
  }
  function closeQuick() {
    const q = $2("edQuick");
    if (!q) return;
    q.classList.remove("open");
    q.setAttribute("aria-hidden", "true");
  }
  function renderQuickList(q) {
    const list = $2("edQuickList");
    const term = q.trim().toLowerCase();
    const items = edQuickIndex.filter((f) => !term || f.path.toLowerCase().includes(term)).slice(0, 40);
    list.innerHTML = items.map((f) => `<div class="ed-quick-item" data-path="${esc(f.path)}"><span class="ed-ico">\u25E6</span><span>${safe(f.path)}</span></div>`).join("");
  }
  var edRunId = null;
  var edStreamES = null;
  var edStreamBuf = "";
  var edTouched = [];
  var edTouchedDiff = /* @__PURE__ */ new Set();
  var edTouchedRe = /(?:[\w.-]+\/)+[\w.-]+\.[A-Za-z0-9]{1,6}/g;
  function noteTouched(text) {
    for (const m of String(text || "").matchAll(edTouchedRe)) {
      const p = m[0].replace(/^\.\//, "");
      if (!edTouched.includes(p)) {
        edTouched.push(p);
        if (edTouched.length > 24) edTouched.shift();
        renderTouched();
      }
    }
  }
  function renderTouched() {
    const host = document.getElementById("edTouched");
    if (!host) return;
    host.innerHTML = edTouched.length ? edTouched.map((p) => `<button type="button" class="chip ghost-chip${edTouchedDiff.has(p) ? " from-diff" : ""}" data-path="${esc(p)}" title="open in editor${edTouchedDiff.has(p) ? " \u2014 changed in the latest run" : ""}">${safe(p)}</button>`).join("") : `<span class="tag">no files touched yet</span>`;
    host.querySelectorAll("button[data-path]").forEach((b) => {
      b.onclick = () => openFile(b.dataset.path);
    });
  }
  function setRunStatus(text, cls) {
    const el2 = $2("edRunStatus");
    if (!el2) return;
    el2.textContent = text;
    el2.classList.remove("live", "working", "err");
    if (cls) el2.classList.add(cls);
    const dot = $2("edLiveDot");
    if (dot) dot.hidden = cls !== "live";
  }
  function fillRunBot(bots) {
    const el2 = $2("edRunBot");
    if (!el2) return;
    const cur = el2.value;
    const opts = (bots || []).map((b) => `<option value="${esc(b.id)}">${esc(b.name)} (${esc(b.route)})</option>`).join("");
    el2.innerHTML = opts ? opts : `<option value="">no teammate deployed</option>`;
    if ([...el2.options].some((o) => o.value === cur)) el2.value = cur;
    const runBtn = $2("edRunBtn");
    if (runBtn) runBtn.disabled = !(bots || []).length;
  }
  async function edRun() {
    const prompt = $2("edPrompt").value.trim();
    if (!prompt) {
      toast("give it a mission first", "err");
      return;
    }
    const bot = $2("edRunBot").value;
    if (!bot) {
      toast("deploy a teammate first", "err");
      return;
    }
    const fullPrompt = prompt + (ctx?.contextBlock() || "");
    try {
      const scope = window.codeStudioScope || {};
      const run = await edApi("POST", "/api/v1/runs", {
        bot_id: bot,
        prompt: fullPrompt,
        intent: "code",
        yolo: $2("edYolo").checked,
        project: scope.project || "",
        tag: scope.tag || ""
      });
      edRunId = run.id;
      edTouched.length = 0;
      edTouchedDiff.clear();
      renderTouched();
      $2("edStopBtn").disabled = false;
      setRunStatus("working\u2026", "working");
      $2("edStream").innerHTML = "";
      renderBrief(prompt, run.id);
      switchTab("stream");
      edConnectStream(run.id);
      pollLiveDiff(run.id);
    } catch (e) {
      toast(safe(e.message), "err");
      setRunStatus("error", "err");
    }
  }
  async function edStop() {
    if (!edRunId) return;
    try {
      await edApi("POST", `/api/v1/runs/${edRunId}/stop`);
      setRunStatus("stopping\u2026", "working");
    } catch (e) {
      toast(safe(e.message), "err");
    }
  }
  function stopLiveDiff() {
    if (ed.liveTimer) {
      clearInterval(ed.liveTimer);
      ed.liveTimer = null;
    }
  }
  function pollLiveDiff(runId) {
    stopLiveDiff();
    ed.liveTimer = setInterval(async () => {
      try {
        const d = await edApi("GET", `/api/v1/runs/${runId}/diff/live`);
        const st = d?.status;
        if (st !== "running" && st !== "queued") {
          stopLiveDiff();
          await loadLatestDiff();
          return;
        }
        if (!d?.files?.length) return;
        const prevSel = ed.sel;
        const prevFocus = ed.focus;
        const prevWorktree = ed.review?.worktree || "";
        ed.review = { ...d, worktree: prevWorktree };
        ed.sel = prevSel;
        ed.focus = prevFocus;
        reconcileHunks(d.files);
        renderDiff();
      } catch {
      }
    }, 2e3);
  }
  async function loadDiffForRun(runId) {
    const d = await edApi("GET", "/api/v1/runs/" + runId + "/diff");
    let meta = {};
    try {
      const runs = await edApi("GET", "/api/v1/runs");
      meta = (runs.runs || runs || []).find((r) => r.id === runId) || {};
    } catch {
    }
    d.judgeScore = meta.judgeScore;
    d.judgeRationale = meta.judgeRationale;
    d.worktree = meta.worktree || meta.worktreePath || "";
    ed.review = d;
    ed.sel = {};
    ed.hunkPrints = {};
    ed.focus = d.files?.length ? [d.files[0].path, 0] : null;
    renderDiff();
    renderReviewState();
    document.querySelector('.nav button[data-panel="editor"]')?.click();
    switchTab("diff");
    return d;
  }
  function edConnectStream(runId) {
    if (edStreamES) {
      try {
        edStreamES.close();
      } catch {
      }
      edStreamES = null;
    }
    const ctrl = new AbortController();
    edStreamES = ctrl;
    fetch("/api/stream?run=" + encodeURIComponent(runId), { signal: ctrl.signal }).then((res) => {
      if (!res.ok) throw new Error("stream " + res.status);
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      const pump = () => reader.read().then(({ done, value }) => {
        if (done) return;
        edStreamBuf += dec.decode(value, { stream: true });
        let idx;
        while ((idx = edStreamBuf.indexOf("\n")) >= 0) {
          const line = edStreamBuf.slice(0, idx);
          edStreamBuf = edStreamBuf.slice(idx + 1);
          edHandleEvent(line);
        }
        return pump();
      });
      return pump();
    }).catch((e) => {
      if (e.name !== "AbortError") console.warn("stream:", e.message);
    });
  }
  function renderBrief(prompt, runId) {
    const box = $2("edStream");
    if (!box) return;
    const card = document.createElement("div");
    card.className = "ed-brief";
    const t = document.createElement("div");
    t.className = "title";
    t.textContent = "Mission";
    card.appendChild(t);
    const b = document.createElement("div");
    b.className = "body";
    b.textContent = prompt || "(untitled)";
    card.appendChild(b);
    const m = document.createElement("div");
    m.className = "meta";
    m.textContent = (/* @__PURE__ */ new Date()).toLocaleTimeString() + (runId ? " \xB7 " + runId.slice(0, 8) : "");
    card.appendChild(m);
    box.appendChild(card);
    box.scrollTop = box.scrollHeight;
  }
  function renderResultCard(status, diff) {
    const box = $2("edStream");
    if (!box) return;
    const card = document.createElement("div");
    card.className = "ed-result " + (status === "done" ? "ok" : "err");
    const t = document.createElement("div");
    t.className = "title";
    t.textContent = status === "done" ? "\u2713 Mission complete" : "\u2717 " + status;
    card.appendChild(t);
    const rows = [];
    const d = diff || ed.review || null;
    const ds = d?.diffSummary || d?.summary;
    if (ds) rows.push(`${ds.files || 0} files \xB7 +${ds.additions ?? 0}/\u2212${ds.deletions ?? 0}`);
    const files = (d?.files || []).map((f) => f.path);
    if (files.length) rows.push(files.slice(0, 5).join(", ") + (files.length > 5 ? ` +${files.length - 5} more` : ""));
    const j = d?.judgeScore;
    if (typeof j === "number" && j > 0) rows.push("\u2605 " + Number(j).toFixed(1) + "/10 \u2014 " + (d?.judgeRationale || ""));
    const b = document.createElement("div");
    b.className = "body";
    b.textContent = rows.join("\n") || "no changes";
    card.appendChild(b);
    const hint = document.createElement("div");
    hint.className = "meta";
    hint.textContent = "review the diff \u2192";
    card.appendChild(hint);
    card.onclick = () => switchTab("diff");
    box.appendChild(card);
    box.scrollTop = box.scrollHeight;
  }
  function edHandleEvent(line) {
    line = line.trim();
    if (!line) return;
    let ev;
    try {
      ev = JSON.parse(line);
    } catch {
      return;
    }
    if (!ev || typeof ev !== "object") return;
    const box = $2("edStream");
    if (!box) return;
    const kind = ev.kind || "";
    const text = String(ev.text || "");
    if (kind === "tool" && (text.length > 240 || text.split("\n").length > 4)) {
      const det = document.createElement("details");
      det.className = "ev tool collapsible";
      const sum = document.createElement("summary");
      const k2 = document.createElement("span");
      k2.className = "k";
      k2.textContent = kind;
      sum.appendChild(k2);
      sum.appendChild(document.createTextNode(" " + sanitize(text.split("\n")[0].slice(0, 80)) + (text.length > 80 ? "\u2026" : "")));
      det.appendChild(sum);
      const body = document.createElement("div");
      body.className = "body";
      body.textContent = text;
      det.appendChild(body);
      box.appendChild(det);
      box.scrollTop = box.scrollHeight;
      noteTouched(text);
      return;
    }
    const div = document.createElement("div");
    div.className = "ev " + kind;
    const k = document.createElement("span");
    k.className = "k";
    k.textContent = kind;
    div.appendChild(k);
    if (ev.text) div.appendChild(document.createTextNode(" " + sanitize(ev.text)));
    box.appendChild(div);
    box.scrollTop = box.scrollHeight;
    if (kind === "tool" || kind === "content") noteTouched(ev.text);
    if (kind === "done" || kind === "run_status") {
      if (/^(done|error|stopped)$/.test(ev.text || "")) {
        setRunStatus(ev.text, ev.text === "done" ? "live" : "err");
        $2("edStopBtn").disabled = true;
        notifyRunDone(ev.text, edRunId);
        loadLatestDiff().then((d) => {
          renderResultCard(ev.text, d);
          switchTab("diff");
        });
        loadCheckpoints();
        loadRuns();
      }
    }
  }
  function notifyRunDone(status, runId) {
    const panelActive = document.getElementById("panel-editor")?.classList.contains("active");
    if (panelActive && !document.hidden) return;
    if (typeof Notification === "undefined") return;
    try {
      if (Notification.permission === "granted") {
        new Notification("flobot run " + status, { body: (runId || "").slice(0, 8) + " \u2014 review the diff in Code Studio" });
      } else if (Notification.permission !== "denied") {
        Notification.requestPermission().then((p) => {
          if (p === "granted") new Notification("flobot run " + status, { body: (runId || "").slice(0, 8) + " \u2014 review the diff in Code Studio" });
        });
      }
    } catch {
    }
  }
  function cpNote(c) {
    const run = c.runId ? String(c.runId).replace(/^run-/, "").slice(0, 8) : "";
    const files = Number(c.files || 0);
    const add = Number(c.additions || 0);
    const del = Number(c.deletions || 0);
    if (!files) return run || c.id || "checkpoint";
    const diff = `+${add} \u2212${del}`;
    return run ? `run ${run} \xB7 ${files} file${files === 1 ? "" : "s"} \xB7 ${diff}` : `${files} file${files === 1 ? "" : "s"} \xB7 ${diff}`;
  }
  async function loadCheckpoints() {
    const box = $2("edCheck");
    if (!box) return;
    try {
      const list = await edApi("GET", "/api/v1/checkpoints");
      const cps = Array.isArray(list) ? list : list && list.checkpoints || [];
      box.innerHTML = cps.length ? cps.map((c) => `<div class="ed-check-item" data-id="${esc(c.id)}">
          <span class="tag">\u21BA</span>
          <span class="ck-note">${safe(c.note || cpNote(c))}</span>
          <span class="ck-time">${safe(c.ts || c.createdAt || "")}</span>
        </div>`).join("") : `<p class="tag">no checkpoints yet.</p>`;
      box.querySelectorAll(".ed-check-item").forEach((item) => {
        item.onclick = async () => {
          try {
            await edApi("POST", `/api/v1/checkpoints/${item.dataset.id}/restore`);
            toast("checkpoint restored", "ok");
            loadTree();
          } catch (e) {
            toast(safe(e.message), "err");
          }
        };
      });
    } catch (e) {
      box.innerHTML = `<p class="tag">${safe(e.message)}</p>`;
    }
  }
  function switchTab(name) {
    document.querySelectorAll(".ed-tab").forEach((t) => t.classList.toggle("sel", t.dataset.tab === name));
    document.querySelectorAll(".ed-tabpane").forEach((p) => p.classList.toggle("sel", p.dataset.pane === name));
  }
  async function pickFolder() {
    if (window.flobotDesktop?.pickFolder) return window.flobotDesktop.pickFolder();
    const bar = $2("edFolderBar");
    if (bar.hidden) {
      bar.hidden = false;
      $2("edFolderPath").focus();
      return void 0;
    }
    bar.hidden = true;
    return void 0;
  }
  async function selectWorkspace(path) {
    path = String(path || "").trim();
    if (!path) {
      toast("type a folder path first", "err");
      return;
    }
    try {
      const r = await edApi("POST", "/api/v1/workspace/select", { path });
      toast("workspace: " + (r.name || r.root));
      ed.openPath = null;
      ed.openSha = null;
      ed.dirty = false;
      ed.descId = null;
      $2("edPath").value = "";
      $2("edContent").value = "";
      $2("edDirty").textContent = "";
      ed.tabs = [];
      ed.activeTab = null;
      renderOpenTabs();
      persistTabs();
      ctx?.clearContext();
      ctx?.renderBreadcrumbs("");
      ctx?.renderOutline("", "");
      edExpanded.clear();
      invalidateQuickIndex();
      $2("edFolderBar").hidden = true;
      await loadTree();
    } catch (e) {
      toast("open folder failed: " + e.message, "err");
    }
  }
  function initEditor() {
    ctx = createCtx({
      ed,
      edApi,
      safe,
      toast,
      openFile,
      getQuickIndex: () => edQuickIndex,
      // The quick-open index is built lazily; the mention picker needs it too,
      // so it asks for a build rather than showing an empty list.
      ensureQuickIndex: () => edQuickIndex.length ? Promise.resolve(edQuickIndex) : buildQuickIndex().then(() => edQuickIndex),
      renderTree,
      edExpanded
    });
    $2("edOpen").onclick = () => openFile($2("edPath").value.trim());
    $2("edSave").onclick = saveFile;
    $2("edRefresh").onclick = loadTree;
    $2("edShowFiles").onclick = async () => {
      edShowAll = !edShowAll;
      try {
        localStorage.setItem("edShowAll", edShowAll ? "1" : "0");
      } catch {
      }
      await renderTree();
    };
    $2("edFolder").onclick = async () => {
      const p = await pickFolder();
      if (p) await selectWorkspace(p);
    };
    $2("edFolderGo").onclick = () => selectWorkspace($2("edFolderPath").value);
    $2("edFolderPath").addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") {
        ev.preventDefault();
        selectWorkspace($2("edFolderPath").value);
      }
      if (ev.key === "Escape") {
        $2("edFolderBar").hidden = true;
      }
    });
    $2("edContent").addEventListener("input", () => {
      if (!ed.openPath) return;
      ed.dirty = true;
      $2("edDirty").textContent = "\u25CF unsaved";
      ctx?.renderOutline($2("edContent").value, ed.openPath);
      snapshotActive();
      renderOpenTabs();
      persistTabs();
    });
    document.addEventListener("keydown", (ev) => {
      if ((ev.metaKey || ev.ctrlKey) && (ev.key === "w" || ev.key === "W")) {
        ev.preventDefault();
        if (ed.activeTab) closeTab(ed.activeTab);
        return;
      }
      if ((ev.metaKey || ev.ctrlKey) && ev.key === "Tab") {
        ev.preventDefault();
        if (!ed.tabs.length) return;
        const i = ed.tabs.findIndex((t) => t.path === ed.activeTab);
        const next = ed.tabs[(i + (ev.shiftKey ? -1 : 1) + ed.tabs.length) % ed.tabs.length];
        activateTab(next.path, false);
      }
    });
    $2("edRunBtn").onclick = edRun;
    $2("edStopBtn").onclick = edStop;
    $2("edRevertBtn").onclick = revertRun;
    $2("edPrompt").addEventListener("keydown", (ev) => {
      if ((ev.metaKey || ev.ctrlKey) && ev.key === "Enter") {
        ev.preventDefault();
        edRun();
      }
    });
    ctx?.wireMention();
    window.addEventListener("pagehide", () => {
      try {
        fetch("/api/v1/workspace/descriptors", { method: "DELETE", keepalive: true }).catch(() => {
        });
      } catch {
      }
    });
    document.querySelectorAll(".ed-tab").forEach((t) => t.onclick = () => switchTab(t.dataset.tab));
    $2("edDiff").addEventListener("click", (ev) => {
      const btn = ev.target.closest("button[data-act]");
      if (!btn) return;
      const hunkEl = btn.closest(".ed-hunk");
      const fi = Number(hunkEl.dataset.fi);
      const hi = Number(hunkEl.dataset.hi);
      const fd = ed.review?.files?.[fi];
      if (!fd) return;
      ed.focus = [fd.path, hi];
      toggleHunk(fd.path, hi, btn.dataset.act === "a");
    });
    $2("edQuickInput").addEventListener("input", (ev) => renderQuickList(ev.target.value));
    $2("edQuickInput").addEventListener("keydown", (ev) => {
      if (ev.key === "Escape") {
        ev.preventDefault();
        closeQuick();
      } else if (ev.key === "Enter") {
        ev.preventDefault();
        const sel = $2("edQuickList").querySelector(".ed-quick-item");
        if (sel) {
          closeQuick();
          openFile(sel.dataset.path);
        }
      }
    });
    $2("edQuickList").addEventListener("click", (ev) => {
      const item = ev.target.closest(".ed-quick-item");
      if (!item) return;
      closeQuick();
      openFile(item.dataset.path);
    });
    window.addEventListener("code-studio:quick-open", () => {
      if (document.getElementById("panel-editor")?.classList.contains("active")) openQuick();
    });
    wireEditorKeys();
    loadTree();
    restoreTabs();
    loadLatestDiff();
    loadCheckpoints();
    loadRuns();
  }
  window.codeStudio = {
    openQuick: () => window.dispatchEvent(new CustomEvent("code-studio:quick-open")),
    openFile,
    fillRunBot,
    // loadDiffForRun opens one run's diff in the review pane. The review
    // queue calls it so a queue entry leads to the changes it names.
    loadDiffForRun,
    // gotoLine jumps the buffer to a 1-based line (used by the outline).
    gotoLine: (n) => ctx?.gotoLine(n),
    refresh: async () => {
      await loadTree();
      await loadLatestDiff();
      await loadCheckpoints();
      await loadRuns();
    }
  };
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initEditor);
  } else {
    initEditor();
  }

  // src/projects.js
  var $p = (id) => document.getElementById(id);
  var pProjects = [];
  var pTags = [];
  var pFilter = "";
  var pScopeId = "";
  var pScopeTree = [];
  async function pApi(method, path, body) {
    const opt = { method, headers: {} };
    if (body !== void 0) {
      opt.headers["Content-Type"] = "application/json";
      opt.body = JSON.stringify(body);
    }
    const res = await fetch(path, opt);
    const text = await res.text();
    let data = null;
    try {
      data = text ? JSON.parse(text) : null;
    } catch {
      data = null;
    }
    if (!res.ok) {
      const msg = data && (data.error || data.message) || res.statusText || "request failed";
      throw new Error(msg);
    }
    return data;
  }
  function pToast(msg, cls) {
    const host = $p("toasts");
    if (!host) return;
    const t = document.createElement("div");
    t.className = "toast" + (cls ? " " + cls : "");
    t.textContent = msg;
    host.appendChild(t);
    setTimeout(() => t.remove(), 2200);
  }
  function pEl(tag, cls, text, onclick) {
    const el2 = document.createElement(tag);
    if (cls) el2.className = cls;
    if (text !== void 0 && text !== null) el2.textContent = String(text);
    if (onclick) el2.onclick = onclick;
    return el2;
  }
  async function pLoad() {
    try {
      const res = await pApi("GET", "/api/v1/projects");
      pProjects = res && res.projects || [];
      pTags = res && res.tags || [];
    } catch (e) {
      pProjects = [];
      pTags = [];
      pToast(e.message, "err");
    }
    pRenderTags();
    pRenderProjects();
    if (pScopeId) await pOpenScope(pScopeId);
  }
  function pRenderTags() {
    const host = $p("edTags");
    if (!host) return;
    host.replaceChildren();
    const all = pEl("button", "ed-tag" + (pFilter === "" ? " on" : ""), "all");
    all.type = "button";
    all.onclick = () => {
      pFilter = "";
      pRenderTags();
      pRenderProjects();
    };
    host.appendChild(all);
    const counts = /* @__PURE__ */ new Map();
    pProjects.forEach((p) => (p.tags || []).forEach((t) => counts.set(t, (counts.get(t) || 0) + 1)));
    pTags.forEach((t) => {
      const name = t.name || t;
      const n = counts.get(name) || 0;
      const b = pEl("button", "ed-tag" + (pFilter.toLowerCase() === name.toLowerCase() ? " on" : ""), "#" + name + " \xB7 " + n);
      b.type = "button";
      b.title = n + (n === 1 ? " project" : " projects");
      b.onclick = () => {
        pFilter = pFilter.toLowerCase() === name.toLowerCase() ? "" : name;
        pRenderTags();
        pRenderProjects();
      };
      host.appendChild(b);
    });
    const add = pEl("button", "ed-tag add", "+ tag");
    add.type = "button";
    add.title = "create a tag";
    add.onclick = pNewTag;
    host.appendChild(add);
  }
  async function pNewTag() {
    const name = window.prompt("Tag name", "");
    if (!name || !name.trim()) return;
    try {
      await pApi("POST", "/api/v1/tags", { name: name.trim() });
      await pLoad();
      pToast("tag added", "ok");
    } catch (e) {
      pToast(e.message, "err");
    }
  }
  function pVisibleProjects() {
    if (!pFilter) return pProjects;
    const f = pFilter.toLowerCase();
    return pProjects.filter((p) => (p.tags || []).some((t) => String(t).toLowerCase() === f));
  }
  function pRenderProjects() {
    const host = $p("edProjects");
    if (!host) return;
    host.replaceChildren();
    const list = pVisibleProjects();
    if (!list.length) {
      host.appendChild(pEl("div", "ed-empty", pFilter ? "no project carries #" + pFilter : "no project yet \u2014 create one to scope the work"));
      return;
    }
    list.forEach((p) => {
      const card = pEl("div", "ed-project");
      card.dataset.project = p.id;
      const top = pEl("div", "ed-project-top");
      const name = pEl("button", "ed-project-name", p.name || "(unnamed)");
      name.type = "button";
      name.title = p.name || "";
      name.onclick = () => pOpenScope(p.id);
      top.appendChild(name);
      top.appendChild(pEl("span", "ed-project-root", p.root || "workspace root"));
      top.appendChild(pEl("span", "spacer"));
      const count = (p.paths || []).length;
      top.appendChild(pEl("span", "ed-project-scope", count ? count + " in scope" : "whole folder"));
      card.appendChild(top);
      const chips = pEl("div", "ed-project-tags");
      (p.tags || []).forEach((t) => {
        const c = pEl("button", "ed-tag small", "#" + t);
        c.type = "button";
        c.title = "remove tag " + t;
        c.onclick = async (ev) => {
          ev.stopPropagation();
          await pSetTags(p, (p.tags || []).filter((x) => x !== t));
        };
        chips.appendChild(c);
      });
      const add = pEl("button", "ed-tag small add", "+ tag");
      add.type = "button";
      add.title = "tag this project";
      add.onclick = async (ev) => {
        ev.stopPropagation();
        const t = window.prompt("Tag this project with", "");
        if (!t || !t.trim()) return;
        await pSetTags(p, (p.tags || []).concat([t.trim()]));
      };
      chips.appendChild(add);
      card.appendChild(chips);
      const actions = pEl("div", "ed-project-actions");
      const scopeBtn = pEl("button", "ed-btn", "files");
      scopeBtn.type = "button";
      scopeBtn.title = "choose the files and folders in scope";
      scopeBtn.onclick = () => pScopeId === p.id ? pCloseScope() : pOpenScope(p.id);
      actions.appendChild(scopeBtn);
      const work = pEl("button", "ed-btn primary", "work on this");
      work.type = "button";
      work.title = "point the next mission at this project";
      work.onclick = () => pWork({ project: p.id, tag: "", label: p.name || "project" });
      actions.appendChild(work);
      const del = pEl("button", "ed-btn ghost", "delete");
      del.type = "button";
      del.onclick = async () => {
        if (!window.confirm("Delete project " + (p.name || "") + "?")) return;
        try {
          await pApi("DELETE", "/api/v1/projects/" + encodeURIComponent(p.id));
          if (pScopeId === p.id) pScopeId = "";
          await pLoad();
          pToast("project deleted", "ok");
        } catch (e) {
          pToast(e.message, "err");
        }
      };
      actions.appendChild(del);
      card.appendChild(actions);
      if (pScopeId === p.id) card.appendChild(pScopeEditor(p));
      host.appendChild(card);
    });
  }
  async function pSetTags(project, tags) {
    try {
      await pApi("PATCH", "/api/v1/projects/" + encodeURIComponent(project.id), { tags });
      await pLoad();
    } catch (e) {
      pToast(e.message, "err");
    }
  }
  function pWork(scope) {
    window.codeStudioScope = scope;
    const prompt = $p("edPrompt");
    if (prompt) {
      prompt.focus();
      const hint = "Work on " + (scope.label || scope.tag || scope.project) + ": ";
      if (!prompt.value.startsWith(hint)) prompt.value = hint + prompt.value;
    }
    pToast("scope set: " + (scope.label || scope.tag || "project"), "ok");
  }
  async function pOpenScope(id) {
    pScopeId = id;
    const p = pProjects.find((x) => x.id === id);
    if (!p) return;
    pScopeTree = [];
    try {
      const res = await fetch("/api/v1/workspace/tree?dir=" + encodeURIComponent(p.root || ""));
      if (res.ok) {
        const body = await res.json();
        pScopeTree = body && body.entries ? body.entries : [];
      }
    } catch {
      pScopeTree = [];
    }
    pRenderProjects();
  }
  function pCloseScope() {
    pScopeId = "";
    pScopeTree = [];
    pRenderProjects();
  }
  function pScopeEditor(project) {
    const box = pEl("div", "ed-scope");
    const selected = new Set(project.paths || []);
    const head = pEl("div", "ed-scope-head");
    head.appendChild(pEl("span", "tag", "files in scope"));
    head.appendChild(pEl("span", "spacer"));
    const save = pEl("button", "ed-btn primary", "save scope");
    save.type = "button";
    save.onclick = async () => {
      try {
        await pApi("PATCH", "/api/v1/projects/" + encodeURIComponent(project.id), { paths: Array.from(selected) });
        await pLoad();
        pToast("scope saved", "ok");
      } catch (e) {
        pToast(e.message, "err");
      }
    };
    head.appendChild(save);
    box.appendChild(head);
    const list = pEl("div", "ed-scope-list");
    const seen = /* @__PURE__ */ new Set();
    const scopeRow = (path, label, isDir, checked) => {
      const row = pEl("label", "ed-scope-item");
      const cb = document.createElement("input");
      cb.type = "checkbox";
      cb.checked = checked;
      cb.onchange = () => {
        if (cb.checked) selected.add(path);
        else selected.delete(path);
      };
      row.appendChild(cb);
      row.appendChild(pEl("span", "ed-ico", isDir ? "\u25B8" : "\u25E6"));
      const text = pEl("span", "ed-scope-path", label);
      text.onclick = () => {
        if (!isDir && window.codeStudio?.openFile) window.codeStudio.openFile(path);
      };
      row.appendChild(text);
      return row;
    };
    (project.paths || []).forEach((path) => {
      seen.add(path);
      list.appendChild(scopeRow(path, path, !/\.[A-Za-z0-9]+$/.test(path), true));
    });
    pScopeTree.forEach((e) => {
      if (seen.has(e.path)) return;
      seen.add(e.path);
      list.appendChild(scopeRow(e.path, e.name || e.path, e.type === "dir", selected.has(e.path)));
    });
    if (!list.childElementCount) {
      list.appendChild(pEl("div", "ed-empty", "nothing to pick here \u2014 the root folder is outside the current workspace"));
    }
    box.appendChild(list);
    return box;
  }
  function pShowNewProject() {
    const host = $p("edProjectForm");
    if (!host) return;
    host.hidden = false;
    host.replaceChildren();
    const name = document.createElement("input");
    name.placeholder = "project name";
    name.id = "edProjName";
    const root = document.createElement("input");
    root.placeholder = "root folder (workspace-relative, . for the whole workspace)";
    root.id = "edProjRoot";
    root.value = ".";
    const paths = document.createElement("input");
    paths.placeholder = "files in scope (comma separated, optional)";
    paths.id = "edProjPaths";
    const tags = document.createElement("input");
    tags.placeholder = "tags (comma separated, optional)";
    tags.id = "edProjTags";
    const row = pEl("div", "ed-form-row");
    [name, root, paths, tags].forEach((i) => row.appendChild(i));
    const actions = pEl("div", "ed-form-row");
    const create = pEl("button", "ed-btn primary", "create project");
    create.type = "button";
    create.onclick = async () => {
      try {
        await pApi("POST", "/api/v1/projects", {
          name: name.value.trim(),
          root: root.value.trim(),
          paths: paths.value.split(",").map((s) => s.trim()).filter(Boolean),
          tags: tags.value.split(",").map((s) => s.trim()).filter(Boolean)
        });
        host.hidden = true;
        host.replaceChildren();
        await pLoad();
        pToast("project created", "ok");
      } catch (e) {
        pToast(e.message, "err");
      }
    };
    actions.appendChild(create);
    const cancel = pEl("button", "ed-btn ghost", "cancel");
    cancel.type = "button";
    cancel.onclick = () => {
      host.hidden = true;
      host.replaceChildren();
    };
    actions.appendChild(cancel);
    host.appendChild(row);
    host.appendChild(actions);
    name.focus();
  }
  function pInitProjects() {
    const newProject = $p("edNewProject");
    if (newProject) newProject.onclick = pShowNewProject;
    const newTag = $p("edNewTag");
    if (newTag) newTag.onclick = pNewTag;
    pLoad();
  }
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", pInitProjects);
  } else {
    pInitProjects();
  }
  window.codeStudioProjects = {
    activeTag: () => pFilter,
    projects: () => pProjects
  };

  // src/find.js
  var $f = (id) => document.getElementById(id);
  var fMatches = [];
  var fIndex = 0;
  var fOpen = false;
  function fBuffer() {
    return $f("edContent");
  }
  function fFind(query) {
    const ta = fBuffer();
    if (!ta) return [];
    const q = String(query || "");
    if (!q) return [];
    const hay = ta.value.toLowerCase();
    const needle = q.toLowerCase();
    const out = [];
    let from = 0;
    while (out.length < 5e3) {
      const i = hay.indexOf(needle, from);
      if (i < 0) break;
      out.push(i);
      from = i + Math.max(1, q.length);
    }
    return out;
  }
  function fRender(select) {
    const ta = fBuffer();
    const count = $f("edFindCount");
    if (!ta || !count) return;
    const q = $f("edFindInput").value;
    fMatches = fFind(q);
    if (!q) {
      count.textContent = "";
      return;
    }
    if (!fMatches.length) {
      count.textContent = "no match";
      return;
    }
    if (fIndex >= fMatches.length) fIndex = 0;
    const pos = fMatches[fIndex];
    const line = ta.value.slice(0, pos).split("\n").length;
    count.textContent = fIndex + 1 + " of " + fMatches.length + " \xB7 line " + line;
    if (!select) return;
    ta.focus();
    ta.setSelectionRange(pos, pos + q.length);
    const lineHeight = parseFloat(getComputedStyle(ta).lineHeight) || 20;
    ta.scrollTop = Math.max(0, (line - 4) * lineHeight);
  }
  function fStep(delta) {
    if (!fMatches.length) {
      fRender(false);
      return;
    }
    fIndex = (fIndex + delta + fMatches.length) % fMatches.length;
    fRender(true);
  }
  function fReplaceCurrent() {
    const ta = fBuffer();
    if (!ta) return;
    const q = $f("edFindInput").value;
    const rep = $f("edReplaceInput").value;
    if (!q || !fMatches.length) return;
    const pos = fMatches[fIndex];
    ta.value = ta.value.slice(0, pos) + rep + ta.value.slice(pos + q.length);
    ta.dispatchEvent(new Event("input", { bubbles: true }));
    fIndex = 0;
    fRender(true);
  }
  function fReplaceAll() {
    const ta = fBuffer();
    if (!ta) return;
    const q = $f("edFindInput").value;
    const rep = $f("edReplaceInput").value;
    if (!q || !fMatches.length) return;
    const n = fMatches.length;
    let out = "";
    let from = 0;
    for (const pos of fMatches) {
      out += ta.value.slice(from, pos) + rep;
      from = pos + q.length;
    }
    out += ta.value.slice(from);
    ta.value = out;
    ta.dispatchEvent(new Event("input", { bubbles: true }));
    fIndex = 0;
    fRender(false);
    fToast(n + (n === 1 ? " replacement" : " replacements"), "ok");
  }
  function fToast(msg, cls) {
    const host = $f("toasts");
    if (!host) return;
    const t = document.createElement("div");
    t.className = "toast" + (cls ? " " + cls : "");
    t.textContent = msg;
    host.appendChild(t);
    setTimeout(() => t.remove(), 2200);
  }
  function fOpenBar() {
    const bar = $f("edFind");
    const input = $f("edFindInput");
    if (!bar || !input) return;
    const ta = fBuffer();
    if (ta && !input.value) {
      const sel = ta.value.slice(ta.selectionStart ?? 0, ta.selectionEnd ?? 0);
      if (sel && sel.length < 120 && !sel.includes("\n")) input.value = sel;
    }
    bar.hidden = false;
    fOpen = true;
    input.focus();
    input.select();
    fRender(false);
  }
  function fCloseBar() {
    const bar = $f("edFind");
    if (!bar) return;
    bar.hidden = true;
    fOpen = false;
    fBuffer()?.focus();
  }
  function fInitFind() {
    const input = $f("edFindInput");
    if (!input) return;
    input.addEventListener("input", () => {
      fIndex = 0;
      fRender(false);
    });
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") {
        ev.preventDefault();
        fStep(ev.shiftKey ? -1 : 1);
      } else if (ev.key === "Escape") {
        ev.preventDefault();
        fCloseBar();
      }
    });
    const rep = $f("edReplaceInput");
    rep?.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") {
        ev.preventDefault();
        fReplaceCurrent();
      } else if (ev.key === "Escape") {
        ev.preventDefault();
        fCloseBar();
      }
    });
    $f("edFindPrev")?.addEventListener("click", () => fStep(-1));
    $f("edFindNext")?.addEventListener("click", () => fStep(1));
    $f("edFindReplace")?.addEventListener("click", fReplaceCurrent);
    $f("edFindReplaceAll")?.addEventListener("click", fReplaceAll);
    $f("edFindClose")?.addEventListener("click", fCloseBar);
  }
  document.addEventListener("keydown", (ev) => {
    if ((ev.metaKey || ev.ctrlKey) && (ev.key === "f" || ev.key === "F")) {
      ev.preventDefault();
      fOpenBar();
      return;
    }
    if (ev.key === "Escape" && fOpen) {
      ev.preventDefault();
      fCloseBar();
    }
  });
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", fInitFind);
  } else {
    fInitFind();
  }

  // src/search.js
  var $s = (id) => document.getElementById(id);
  var sMatches = [];
  var sQuery = "";
  var sTruncated = false;
  function sScope() {
    const tag = window.codeStudioProjects?.activeTag?.() || "";
    return tag;
  }
  async function sSearch(query) {
    sQuery = String(query || "");
    const host = $s("edSearchResults");
    if (!host) return;
    if (!sQuery.trim()) {
      sMatches = [];
      host.replaceChildren();
      const note = document.createElement("div");
      note.className = "ed-empty";
      note.textContent = "search the workspace by text";
      host.appendChild(note);
      return;
    }
    const tag = sScope();
    try {
      const url = "/api/v1/workspace/search?q=" + encodeURIComponent(sQuery) + (tag ? "&tag=" + encodeURIComponent(tag) : "");
      const res = await fetch(url);
      const body = await res.json();
      sMatches = body.matches || [];
      sTruncated = !!body.truncated;
    } catch (e) {
      sMatches = [];
      host.replaceChildren();
      const note = document.createElement("div");
      note.className = "ed-empty";
      note.textContent = "search failed";
      host.appendChild(note);
      return;
    }
    host.replaceChildren();
    const scopeTag = sScope();
    const head = document.createElement("div");
    head.className = "ed-search-head";
    head.textContent = sMatches.length + (sMatches.length === 1 ? " hit" : " hits") + (scopeTag ? " in #" + scopeTag : "") + (sTruncated ? " \xB7 showing the first 200" : "");
    host.appendChild(head);
    if (!sMatches.length) {
      const note = document.createElement("div");
      note.className = "ed-empty";
      note.textContent = "no match for \u201C" + sQuery + "\u201D";
      host.appendChild(note);
      return;
    }
    sMatches.forEach((m) => {
      const row = document.createElement("button");
      row.type = "button";
      row.className = "ed-search-item";
      const where = document.createElement("span");
      where.className = "ed-search-where";
      where.textContent = m.path + ":" + m.line;
      const text = document.createElement("span");
      text.className = "ed-search-text";
      text.textContent = m.text.trim();
      row.appendChild(where);
      row.appendChild(text);
      row.onclick = async () => {
        if (window.codeStudio?.openFile) await window.codeStudio.openFile(m.path);
        if (m.line > 0 && window.codeStudio?.gotoLine) window.codeStudio.gotoLine(m.line);
      };
      host.appendChild(row);
    });
  }
  function sInitSearch() {
    const input = $s("edSearchInput");
    if (!input) return;
    let timer = null;
    input.addEventListener("input", () => {
      clearTimeout(timer);
      timer = setTimeout(() => sSearch(input.value), 180);
    });
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") {
        ev.preventDefault();
        clearTimeout(timer);
        sSearch(input.value);
      } else if (ev.key === "Escape") {
        ev.preventDefault();
        input.value = "";
        sSearch("");
        input.blur();
      }
    });
  }
  document.addEventListener("keydown", (ev) => {
    if ((ev.metaKey || ev.ctrlKey) && ev.shiftKey && (ev.key === "f" || ev.key === "F")) {
      ev.preventDefault();
      const input = $s("edSearchInput");
      if (input) {
        input.focus();
        input.select();
      }
    }
  });
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", sInitSearch);
  } else {
    sInitSearch();
  }

  // src/examples.js
  var MISSIONS = [
    {
      icon: "\u{1F41B}",
      title: "Squash the flaky test",
      intent: "code",
      prompt: "Find the flaky test in the suite (it fails ~1 in 5 runs), figure out the root cause, and fix it. Run the suite 10 times to prove it's stable."
    },
    {
      icon: "\u{1F9F9}",
      title: "Dependency dusting",
      intent: "code",
      prompt: "Audit go.mod for outdated and indirect dependencies, upgrade the safe ones, run the full test suite, and summarize what changed and why."
    },
    {
      icon: "\u{1F50D}",
      title: "Explain this codebase",
      intent: "ask",
      prompt: "Map this repo: what it does, the main packages, where the entrypoints are, and the three riskiest files. Write it up like you're onboarding a new engineer."
    },
    {
      icon: "\u{1F5FA}\uFE0F",
      title: "Draft a migration plan",
      intent: "plan",
      prompt: "Plan the migration of our REST handlers to the new router library. List every touchpoint, the order of work, and the rollback story. Don't change code yet."
    },
    {
      icon: "\u26A1",
      title: "Chase the slow endpoint",
      intent: "code",
      prompt: "Profile the slowest HTTP handler, find the hot path, and optimize it. Show before/after timings from a benchmark."
    },
    {
      icon: "\u{1F4DD}",
      title: "Write the missing docs",
      intent: "code",
      prompt: "Find every exported function in the internal packages with a missing or stub doc comment and write proper Go doc comments for each."
    },
    {
      icon: "\u{1F6E1}\uFE0F",
      title: "Hunt the race",
      intent: "code",
      prompt: "Run the tests with -race, reproduce any data races, fix them, and prove the fix with three clean -race runs."
    },
    {
      icon: "\u{1F3F7}\uFE0F",
      title: "Cut a changelog",
      intent: "ask",
      prompt: "Read the git log since the last release tag and draft a changelog grouped by feature / fix / chore, written for humans."
    }
  ];
  var SKILL_TEMPLATES = [
    {
      name: "nightly-guard",
      body: `# nightly-guard

Every night, defend the repo:

1. Run the full test suite.
2. If anything fails, open a worktree, reproduce, and attempt a fix.
3. Post a summary: what broke, why, what you did.
4. If the tree is green, say so in one line and go back to sleep.`
    },
    {
      name: "pr-triage",
      body: `# pr-triage

Triage open PRs:

1. List open PRs with failing or stale checks.
2. For each: summarize the change in one sentence, flag risky diffs.
3. Label: needs-review, needs-fixes, or ship-it.
4. Never merge anything \u2014 humans push the button.`
    },
    {
      name: "dep-sweep",
      body: `# dep-sweep

Weekly dependency hygiene:

1. List outdated direct deps.
2. Upgrade patch and minor versions only; leave majors alone.
3. Run tests after each bump; revert on any failure.
4. Report the final diff summary.`
    },
    {
      name: "release-notes",
      body: `# release-notes

When a release is cut:

1. Diff since the last tag.
2. Group changes: features, fixes, chores.
3. Write notes for humans, not commits.
4. Flag anything that needs a migration callout.`
    }
  ];
  var ROUTINE_TRIGGERS = [
    { label: "manual", value: "manual" },
    { label: "every 5 minutes", value: "every 5m" },
    { label: "every 15 minutes", value: "every 15m" },
    { label: "every hour", value: "every 1h" },
    { label: "every 6 hours", value: "every 6h" },
    { label: "every day", value: "every 1d" },
    { label: "every minute", value: "cron * * * * *" },
    { label: "every 15 min", value: "cron */15 * * * *" },
    { label: "hourly", value: "cron @hourly" },
    { label: "daily 9am", value: "cron 0 9 * * *" },
    { label: "weekdays 9am", value: "cron 0 9 * * 1-5" }
  ];
  var DEMO_SCRIPT = [
    { delay: 200, ev: { kind: "run_status", text: "demo run started \xB7 bot Coder \xB7 intent code" } },
    { delay: 500, ev: { kind: "thinking", text: "The user wants the flaky test fixed. First I should find which test is flaky \u2014 let me run the suite a few times and look for non-determinism before touching anything." } },
    { delay: 700, ev: { kind: "plan", text: "1. Run suite 5\xD7 to catch the flake\n2. Read the failing test + its fixtures\n3. Find the race or timing assumption\n4. Fix, then prove with 10 clean runs" } },
    { delay: 600, ev: { kind: "tool_start", tool: "bash" } },
    { delay: 900, ev: { kind: "tool_end", tool: "bash", text: "go test ./... -count=5 \u2192 1 failure in TestRosterSave (run 3 of 5)" } },
    { delay: 500, ev: { kind: "content", text: "Caught it: TestRosterSave fails ~40% of runs. The test writes to a shared roster.json without a lock \u2014 two goroutines race on the file." } },
    { delay: 600, ev: { kind: "tool_start", tool: "read" } },
    { delay: 800, ev: { kind: "tool_end", tool: "read", text: "internal/roster/store.go:88 \u2014 concurrent map write, no mutex" } },
    { delay: 500, ev: { kind: "perm", text: "edit internal/roster/store.go (add sync.Mutex around save)" } },
    { delay: 900, ev: { kind: "tool_start", tool: "edit" } },
    { delay: 700, ev: { kind: "tool_end", tool: "edit", text: "@@ internal/roster/store.go @@\n-func (s *Store) save() error {\n+func (s *Store) save() error {\n+	s.mu.Lock()\n+	defer s.mu.Unlock()" } },
    { delay: 600, ev: { kind: "tool_start", tool: "bash" } },
    { delay: 1100, ev: { kind: "tool_end", tool: "bash", text: "go test ./... -count=10 -race \u2192 10/10 green" } },
    { delay: 500, ev: { kind: "verify", text: "suite green 10\xD7 under -race" } },
    { delay: 600, ev: { kind: "content", text: "Fixed. The save path now holds a mutex; ten consecutive -race runs are green. Diff is two lines." } },
    { delay: 700, ev: { kind: "done", text: "demo complete \u2014 that's flobot. now give a real mission." } }
  ];
  function playDemo(appendEvent2, onDone) {
    let cancelled = false;
    let timer = null;
    let i = 0;
    const step = () => {
      if (cancelled) return;
      if (i >= DEMO_SCRIPT.length) {
        if (onDone) onDone();
        return;
      }
      const { delay, ev } = DEMO_SCRIPT[i++];
      timer = setTimeout(() => {
        appendEvent2(ev);
        step();
      }, delay);
    };
    step();
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }

  // src/power.js
  function fuzzy(needle, hay) {
    needle = needle.toLowerCase();
    hay = String(hay).toLowerCase();
    if (!needle) return 0;
    let score = 0, ni = 0, streak = 0;
    for (let i = 0; i < hay.length && ni < needle.length; i++) {
      if (hay[i] === needle[ni]) {
        ni++;
        streak++;
        score += 1 + streak;
        if (i === 0 || " /-_.:".includes(hay[i - 1])) score += 5;
      } else streak = 0;
    }
    return ni === needle.length ? score : -1;
  }
  var Palette = class {
    // commands: [{label, hint, icon, run}]
    constructor(opts) {
      this.commands = opts.commands || [];
      this.onClose = opts.onClose || (() => {
      });
      this.sel = 0;
      this.results = [];
      this.build();
    }
    build() {
      this.root = document.createElement("div");
      this.root.className = "palette-root";
      this.root.innerHTML = `
      <div class="palette" role="dialog" aria-label="command palette">
        <div class="palette-in">
          <span class="p-ico">\u2318</span>
          <input class="p-input" placeholder="type a command\u2026" spellcheck="false" autocomplete="off">
        </div>
        <div class="p-list"></div>
        <div class="p-foot">
          <span><b>\u2191\u2193</b> navigate</span><span><b>\u21B5</b> run</span><span><b>esc</b> close</span>
        </div>
      </div>`;
      document.body.appendChild(this.root);
      this.input = this.root.querySelector(".p-input");
      this.list = this.root.querySelector(".p-list");
      this.input.addEventListener("input", () => this.search(this.input.value));
      this.input.addEventListener("keydown", (e) => {
        if (e.key === "ArrowDown") {
          e.preventDefault();
          this.move(1);
        } else if (e.key === "ArrowUp") {
          e.preventDefault();
          this.move(-1);
        } else if (e.key === "Enter") {
          e.preventDefault();
          this.exec();
        } else if (e.key === "Escape") {
          e.preventDefault();
          this.close();
        }
      });
      this.root.addEventListener("mousedown", (e) => {
        if (e.target === this.root) this.close();
      });
      this.search("");
      this.input.focus();
    }
    setCommands(cmds) {
      this.commands = cmds;
      this.search(this.input.value);
    }
    search(q) {
      const scored = this.commands.map((c) => ({ c, s: fuzzy(q, c.label + " " + (c.hint || "")) })).filter((x) => x.s >= 0).sort((a, b) => b.s - a.s).slice(0, 12);
      this.results = scored.map((x) => x.c);
      this.sel = 0;
      this.render();
    }
    render() {
      this.list.innerHTML = this.results.map((c, i) => `
      <button type="button" class="p-item ${i === this.sel ? "sel" : ""}" data-i="${i}">
        <span class="p-item-ico">${c.icon || "\u203A"}</span>
        <span class="p-item-label">${c.label}</span>
        ${c.hint ? `<span class="p-item-hint">${c.hint}</span>` : ""}
      </button>`).join("") || `<div class="p-empty">nothing matches \u2014 try "run", "deploy", "demo"</div>`;
      this.list.querySelectorAll("[data-i]").forEach((btn) => {
        btn.onclick = () => {
          this.sel = +btn.dataset.i;
          this.exec();
        };
        btn.onmouseenter = () => {
          this.sel = +btn.dataset.i;
          this.render();
        };
      });
      const sel = this.list.querySelector(".sel");
      if (sel) sel.scrollIntoView({ block: "nearest" });
    }
    move(d) {
      if (!this.results.length) return;
      this.sel = (this.sel + d + this.results.length) % this.results.length;
      this.render();
    }
    exec() {
      const c = this.results[this.sel];
      if (!c) return;
      this.close();
      c.run();
    }
    close() {
      if (this.root) {
        this.root.remove();
        this.root = null;
      }
      this.onClose();
    }
  };
  function boot() {
    if (localStorage.getItem("flobot-booted")) return;
    if (matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const el2 = document.createElement("div");
    el2.className = "boot";
    const lines = [
      "flobot v1 \xB7 standalone \xB7 loopback only",
      "loading roster \u2026",
      "mounting skills library \u2026",
      "arming routines \u2026",
      "probing model routes \u2026",
      "crew online."
    ];
    el2.innerHTML = `<div class="boot-in"><pre class="boot-lines"></pre></div>`;
    document.body.appendChild(el2);
    const pre = el2.querySelector(".boot-lines");
    let i = 0;
    const step = () => {
      if (i < lines.length) {
        pre.textContent += lines[i++] + "\n";
        setTimeout(step, 160);
      } else {
        setTimeout(() => {
          el2.classList.add("out");
          localStorage.setItem("flobot-booted", "1");
          setTimeout(() => el2.remove(), 500);
        }, 350);
      }
    };
    step();
  }
  function confetti() {
    if (matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const cv = document.createElement("canvas");
    cv.className = "confetti-cv";
    cv.width = innerWidth;
    cv.height = innerHeight;
    document.body.appendChild(cv);
    const ctx2 = cv.getContext("2d");
    const colors = ["#6366f1", "#22d3ee", "#4ee6a8", "#e879f9", "#fbbf24"];
    const parts = Array.from({ length: 120 }, () => ({
      x: Math.random() * cv.width,
      y: -20 - Math.random() * 80,
      vx: (Math.random() - 0.5) * 3,
      vy: 2 + Math.random() * 3,
      w: 6 + Math.random() * 6,
      h: 3 + Math.random() * 4,
      rot: Math.random() * Math.PI,
      vr: (Math.random() - 0.5) * 0.3,
      c: colors[Math.random() * colors.length | 0]
    }));
    const t0 = performance.now();
    const frame = (t) => {
      const dt = t - t0;
      ctx2.clearRect(0, 0, cv.width, cv.height);
      for (const p of parts) {
        p.x += p.vx;
        p.y += p.vy;
        p.rot += p.vr;
        ctx2.save();
        ctx2.translate(p.x, p.y);
        ctx2.rotate(p.rot);
        ctx2.fillStyle = p.c;
        ctx2.fillRect(-p.w / 2, -p.h / 2, p.w, p.h);
        ctx2.restore();
      }
      if (dt < 2200) requestAnimationFrame(frame);
      else cv.remove();
    };
    requestAnimationFrame(frame);
  }

  // src/telemetry.js
  var Stats = class {
    constructor() {
      this.t0 = Date.now();
      this.counts = {};
      this.recent = [];
    }
    bump(kind) {
      this.counts[kind] = (this.counts[kind] || 0) + 1;
      this.recent.push([Date.now(), kind]);
      const cutoff = Date.now() - 6e4;
      while (this.recent.length && this.recent[0][0] < cutoff) this.recent.shift();
    }
    total() {
      return Object.values(this.counts).reduce((a, b) => a + b, 0);
    }
    perMin() {
      return this.recent.length;
    }
    tools() {
      return this.counts["tool_start"] || 0;
    }
    errs() {
      return this.counts["err"] || 0;
    }
  };
  function timeline(events) {
    const phases = [];
    let cur = null;
    for (const e of events) {
      const p = e.kind === "thinking" ? "think" : e.kind === "tool_start" || e.kind === "tool_end" ? "tool" : e.kind === "content" ? "write" : null;
      if (!p) continue;
      if (cur && cur.p === p) {
        cur.n++;
        continue;
      }
      cur = { p, n: 1 };
      phases.push(cur);
    }
    const total = phases.reduce((a, x) => a + x.n, 0) || 1;
    return phases.map((x) => ({ ...x, frac: x.n / total }));
  }
  function isDiff(text) {
    if (!text || typeof text !== "string") return false;
    const lines = text.split("\n");
    if (lines.length < 2) return false;
    let add = 0, del = 0;
    for (const l of lines) {
      if (l.startsWith("+") && !l.startsWith("+++")) add++;
      else if (l.startsWith("-") && !l.startsWith("---")) del++;
    }
    return add + del >= 2 && add + del >= lines.length * 0.3;
  }
  function renderDiff2(text) {
    const rows = [];
    for (const l of String(text).split("\n")) {
      if (l.startsWith("+++") || l.startsWith("---")) continue;
      if (l.startsWith("@@")) {
        rows.push(`<span class="dl hl">${esc2(l)}</span>`);
        continue;
      }
      if (l.startsWith("+")) rows.push(`<span class="dl add">+ ${esc2(l.slice(1))}</span>`);
      else if (l.startsWith("-")) rows.push(`<span class="dl del">- ${esc2(l.slice(1))}</span>`);
      else rows.push(`<span class="dl">  ${esc2(l)}</span>`);
    }
    return `<div class="diff">${rows.join("")}</div>`;
  }
  function esc2(s) {
    return String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
  }
  var Sfx = class {
    constructor() {
      this.on = false;
      this.ctx = null;
    }
    ensure() {
      if (!this.ctx) {
        try {
          this.ctx = new (window.AudioContext || window.webkitAudioContext)();
        } catch {
          this.ctx = null;
        }
      }
      if (this.ctx && this.ctx.state === "suspended") this.ctx.resume();
      return this.ctx;
    }
    tone(freq, dur, delay = 0, type = "sine", gain = 0.04) {
      const ctx2 = this.ensure();
      if (!ctx2 || !this.on) return;
      const t = ctx2.currentTime + delay;
      const o = ctx2.createOscillator();
      const g = ctx2.createGain();
      o.type = type;
      o.frequency.value = freq;
      g.gain.setValueAtTime(gain, t);
      g.gain.exponentialRampToValueAtTime(1e-4, t + dur);
      o.connect(g);
      g.connect(ctx2.destination);
      o.start(t);
      o.stop(t + dur);
    }
    start() {
      this.tone(392, 0.09);
      this.tone(523.25, 0.12, 0.07);
    }
    done() {
      this.tone(523.25, 0.1);
      this.tone(659.25, 0.14, 0.08);
      this.tone(783.99, 0.18, 0.16);
    }
    fail() {
      this.tone(196, 0.22, 0, "sawtooth", 0.05);
    }
    tick() {
      this.tone(880, 0.03, 0, "square", 0.015);
    }
  };
  function toMarkdown(run, events, botName) {
    const L = [];
    L.push(`# flobot run ${run.id.slice(0, 8)}`);
    L.push("");
    L.push(`- **bot:** ${botName || run.bot_id || "?"}`);
    L.push(`- **intent:** ${run.intent || "?"}`);
    L.push(`- **status:** ${run.status}`);
    if (run.error) L.push(`- **error:** ${run.error}`);
    L.push(`- **events:** ${events.length}`);
    L.push("");
    for (const e of events) {
      switch (e.kind) {
        case "content":
          L.push(e.text, "");
          break;
        case "thinking":
          L.push(`<details><summary>thinking</summary>`, "", e.text, "", `</details>`, "");
          break;
        case "tool_start":
          L.push(`**\u2192 ${e.tool}**`);
          break;
        case "tool_end":
          if (e.text) L.push("```", e.text, "```", "");
          break;
        case "perm":
          L.push(`> perm: ${e.text}`, "");
          break;
        case "plan":
          L.push("**plan**", "", e.text, "");
          break;
        case "err":
          L.push(`**error:** ${e.text}`, "");
          break;
        case "done":
          L.push(`\u2014 ${e.text} \u2014`, "");
          break;
        case "verify":
          L.push(e.is_error ? `**verify \u2717** ${e.text}` : `**verify \u2713** ${e.text}`, "");
          break;
        default:
          if (e.text) L.push(`_${e.kind}: ${e.text}_`, "");
      }
    }
    return L.join("\n");
  }

  // src/vibe.js
  function vibe() {
    if (matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const cv = document.createElement("canvas");
    cv.className = "starfield";
    document.body.prepend(cv);
    const ctx2 = cv.getContext("2d");
    let W, H, stars = [];
    const resize = () => {
      W = cv.width = innerWidth;
      H = cv.height = innerHeight;
      stars = Array.from({ length: Math.min(160, W * H / 12e3) }, () => ({
        x: Math.random() * W,
        y: Math.random() * H,
        r: Math.random() * 1.3 + 0.3,
        vy: 0.06 + Math.random() * 0.25,
        tw: Math.random() * Math.PI * 2
      }));
    };
    resize();
    addEventListener("resize", resize);
    const glow = document.createElement("div");
    glow.className = "cursor-glow";
    document.body.appendChild(glow);
    let mx = innerWidth / 2, my = innerHeight / 3;
    addEventListener("pointermove", (e) => {
      mx = e.clientX;
      my = e.clientY;
    });
    let last = 0;
    const frame = (t) => {
      requestAnimationFrame(frame);
      if (document.hidden) return;
      if (t - last < 40) return;
      last = t;
      ctx2.clearRect(0, 0, W, H);
      for (const s of stars) {
        s.y += s.vy;
        s.tw += 0.04;
        if (s.y > H + 2) {
          s.y = -2;
          s.x = Math.random() * W;
        }
        const a = 0.25 + 0.55 * Math.abs(Math.sin(s.tw));
        ctx2.beginPath();
        ctx2.arc(s.x, s.y, s.r, 0, 7);
        ctx2.fillStyle = `rgba(165,180,252,${a})`;
        ctx2.fill();
      }
      glow.style.transform = `translate(${mx - 260}px, ${my - 260}px)`;
    };
    requestAnimationFrame(frame);
  }

  // src/supervision.js
  var $3 = (id) => document.getElementById(id);
  async function api(method, path, body) {
    const opt = { method, headers: {} };
    if (body !== void 0) {
      opt.headers["Content-Type"] = "application/json";
      opt.body = JSON.stringify(body);
    }
    const r = await fetch(path, opt);
    const text = await r.text();
    let data = null;
    try {
      data = text ? JSON.parse(text) : null;
    } catch {
      data = null;
    }
    if (!r.ok) throw new Error(data && data.error || r.statusText || "request failed");
    return data;
  }
  var CSS2 = `
.sv-card{position:fixed;right:18px;bottom:18px;z-index:60;width:290px;
  background:var(--panel,#16181d);border:1px solid var(--line,#2a2e36);border-radius:12px;
  box-shadow:0 12px 32px rgba(0,0,0,.45);color:var(--fg,#e7e9ee);font-size:12px;
  display:flex;flex-direction:column;max-height:60vh;overflow:hidden}
.sv-head{display:flex;align-items:center;gap:8px;padding:8px 10px;border-bottom:1px solid var(--line,#2a2e36);
  font-weight:600;letter-spacing:.02em;cursor:pointer;user-select:none}
.sv-badge{margin-left:auto;min-width:18px;height:18px;border-radius:9px;background:#e0603a;color:#fff;
  font-size:11px;line-height:18px;text-align:center;font-weight:700}
.sv-badge[hidden]{display:none}
.sv-body{padding:6px 10px 10px;overflow:auto}
.sv-body[hidden]{display:none}
.sv-empty{opacity:.55;padding:4px 0}
.sv-row{display:flex;align-items:baseline;gap:6px;padding:3px 0;cursor:pointer;border-radius:4px}
.sv-row:hover{background:rgba(255,255,255,.05)}
.sv-dot{width:7px;height:7px;border-radius:50%;flex:0 0 auto;background:#6b7280}
.sv-dot.running{background:#e0a83a}.sv-dot.done{background:#3aa76d}.sv-dot.failed{background:#d4483b}
.sv-goal{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;flex:1}
.sv-kind{opacity:.5;font-size:10px;text-transform:uppercase;letter-spacing:.04em}
.sv-ln{opacity:.5;font-size:10px}
.sv-pal{position:fixed;inset:0;z-index:90;background:rgba(0,0,0,.45);display:flex;
  align-items:flex-start;justify-content:center;padding-top:12vh}
.sv-pal[hidden]{display:none}
.sv-pal-box{width:min(640px,92vw);background:var(--panel,#16181d);border:1px solid var(--line,#2a2e36);
  border-radius:12px;box-shadow:0 18px 48px rgba(0,0,0,.55);overflow:hidden;color:var(--fg,#e7e9ee)}
.sv-pal-in{width:100%;box-sizing:border-box;padding:12px 14px;border:0;border-bottom:1px solid var(--line,#2a2e36);
  background:transparent;color:inherit;font:inherit;font-size:14px;outline:none}
.sv-pal-list{max-height:46vh;overflow:auto;padding:4px}
.sv-pal-item{display:flex;align-items:baseline;gap:8px;padding:6px 10px;border-radius:6px;cursor:pointer}
.sv-pal-item:hover,.sv-pal-item.sel{background:rgba(255,255,255,.07)}
.sv-pal-path{opacity:.6;font-size:11px;margin-left:auto}
.sv-act{margin-left:4px;border:0;background:transparent;color:inherit;cursor:pointer;font-size:11px;opacity:.6;padding:0 3px;border-radius:3px}
.sv-act:hover{opacity:1;background:rgba(255,255,255,.1)}
.sv-note{opacity:.55;padding:8px 10px;font-size:11px}
.rtn-edit{margin-top:6px;display:flex;gap:6px;align-items:center}
.rtn-edit[hidden]{display:none}
.rtn-edit-in{flex:1;min-width:120px;background:rgba(0,0,0,.25);border:1px solid var(--line,#2a2e36);
  border-radius:6px;color:inherit;font:inherit;font-size:12px;padding:4px 7px;outline:none}
.rtn-edit-in:focus{border-color:#4b5563}
`;
  function injectStyles() {
    if (document.getElementById("sv-styles")) return;
    const s = document.createElement("style");
    s.id = "sv-styles";
    s.textContent = CSS2;
    document.head.appendChild(s);
  }
  var state = { threads: [], queue: [], pending: 0, open: false };
  function el(tag, cls, text) {
    const n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text !== void 0) n.textContent = text;
    return n;
  }
  function toast2(msg, cls) {
    let box = document.getElementById("sv-toasts");
    if (!box) {
      box = el("div");
      box.id = "sv-toasts";
      box.style.cssText = "position:fixed;right:18px;bottom:120px;z-index:95;display:flex;flex-direction:column;gap:6px;pointer-events:none";
      document.body.appendChild(box);
    }
    const t = el("div", "", msg);
    t.style.cssText = "background:#22262e;color:#e7e9ee;border:1px solid #343a44;border-radius:8px;padding:7px 11px;font-size:12px;max-width:280px";
    if (cls === "err") t.style.borderColor = "#d4483b";
    box.appendChild(t);
    setTimeout(() => t.remove(), 2600);
  }
  function render() {
    const body = document.querySelector("#sv-card .sv-body");
    if (!body) return;
    body.textContent = "";
    const q = el("div", "sv-row");
    q.appendChild(el("span", "sv-dot", ""));
    q.appendChild(el("span", "sv-goal", `Review queue \u2014 ${state.pending} pending`));
    body.appendChild(q);
    if (state.pending === 0) {
      body.appendChild(el("div", "sv-empty", "Nothing waiting for review."));
    } else {
      for (const e of state.queue) {
        if (e.state === "resolved") continue;
        const row = el("div", "sv-row");
        row.appendChild(el("span", "sv-dot", ""));
        const goal = el("span", "sv-goal", e.goal || e.run_id || "run");
        row.appendChild(goal);
        row.appendChild(el("span", "sv-kind", `${e.files_changed ? e.files_changed.length : 0} files`));
        const open = el("button", "sv-act", "open");
        open.title = "open this run's diff";
        open.onclick = async (ev) => {
          ev.stopPropagation();
          try {
            await window.codeStudio?.loadDiffForRun?.(e.run_id);
            await api("POST", `/api/v1/queue/${encodeURIComponent(e.run_id)}/state`, { state: "in_review" });
            await refresh();
          } catch (err) {
            toast2("could not open the diff: " + (err && err.message ? err.message : "unknown error"), "err");
          }
        };
        row.appendChild(open);
        const done = el("button", "sv-act", "\u2713");
        done.title = "mark reviewed";
        done.onclick = async (ev) => {
          ev.stopPropagation();
          try {
            await api("DELETE", `/api/v1/queue/${encodeURIComponent(e.run_id)}`);
            toast2("resolved");
            await refresh();
          } catch (err) {
            toast2("could not resolve: " + (err && err.message ? err.message : "unknown error"), "err");
          }
        };
        row.appendChild(done);
        row.title = e.run_id || "";
        body.appendChild(row);
      }
    }
    body.appendChild(el("div", "sv-empty", ""));
    body.appendChild(el("div", "sv-kind", "threads"));
    if (!state.threads.length) {
      body.appendChild(el("div", "sv-empty", "No threads yet."));
    }
    for (const t of state.threads) {
      const row = el("div", "sv-row");
      const dot = el("span", "sv-dot", "");
      dot.className = "sv-dot " + (t.status || "");
      row.appendChild(dot);
      row.appendChild(el("span", "sv-goal", t.goal || t.id));
      const kill = el("button", "sv-act", "\u2715");
      kill.title = "remove this thread and its worktree";
      kill.onclick = async (ev) => {
        ev.stopPropagation();
        if (!confirm("Remove thread " + (t.id || "") + " and delete its worktree?\n\nAnything in the worktree that was not merged into your tree is lost.")) return;
        try {
          await api("DELETE", `/api/v1/threads/${encodeURIComponent(t.id)}`);
          toast2("thread removed");
          await refresh();
        } catch (err) {
          toast2("could not remove: " + (err && err.message ? err.message : "unknown error"), "err");
        }
      };
      row.appendChild(kill);
      row.title = t.worktree_path || t.id || "";
      body.appendChild(row);
    }
    const badge = document.querySelector("#sv-card .sv-badge");
    if (badge) {
      badge.textContent = String(state.pending);
      badge.hidden = state.pending === 0;
    }
  }
  async function refresh() {
    try {
      const [t, q] = await Promise.all([
        api("GET", "/api/v1/threads"),
        api("GET", "/api/v1/queue")
      ]);
      state.threads = t && t.threads || [];
      state.queue = q && q.entries || [];
      state.pending = q && q.pending || 0;
      render();
    } catch {
    }
  }
  function buildCard() {
    if (document.getElementById("sv-card")) return;
    const card = el("div", "sv-card");
    card.id = "sv-card";
    const head = el("div", "sv-head");
    head.appendChild(el("span", "", "Supervision"));
    const badge = el("span", "sv-badge", "0");
    badge.hidden = true;
    head.appendChild(badge);
    const body = el("div", "sv-body");
    body.hidden = true;
    head.onclick = () => {
      state.open = !state.open;
      body.hidden = !state.open;
    };
    card.appendChild(head);
    card.appendChild(body);
    document.body.appendChild(card);
  }
  var pal = { open: false, items: [], sel: 0, seq: 0 };
  function buildPalette() {
    if (document.getElementById("sv-pal")) return;
    const wrap = el("div", "sv-pal");
    wrap.id = "sv-pal";
    wrap.hidden = true;
    const box = el("div", "sv-pal-box");
    const input = document.createElement("input");
    input.className = "sv-pal-in";
    input.placeholder = "Go to symbol\u2026  (name of a function, type or class)";
    input.id = "sv-pal-input";
    const list = el("div", "sv-pal-list");
    list.id = "sv-pal-list";
    box.appendChild(input);
    box.appendChild(list);
    wrap.appendChild(box);
    wrap.onclick = (ev) => {
      if (ev.target === wrap) closePalette();
    };
    document.body.appendChild(wrap);
    let timer = null;
    input.oninput = () => {
      clearTimeout(timer);
      timer = setTimeout(() => searchSymbols(input.value), 120);
    };
    input.onkeydown = (ev) => {
      if (ev.key === "Escape") {
        closePalette();
      } else if (ev.key === "ArrowDown") {
        ev.preventDefault();
        pal.sel = Math.min(pal.sel + 1, pal.items.length - 1);
        paintPalette();
      } else if (ev.key === "ArrowUp") {
        ev.preventDefault();
        pal.sel = Math.max(pal.sel - 1, 0);
        paintPalette();
      } else if (ev.key === "Enter") {
        ev.preventDefault();
        pickSymbol(pal.sel);
      }
    };
  }
  function paintPalette(message) {
    const list = document.getElementById("sv-pal-list");
    if (!list) return;
    list.textContent = "";
    if (message) {
      list.appendChild(el("div", "sv-note", message));
      return;
    }
    if (!pal.items.length) {
      list.appendChild(el("div", "sv-note", "No symbols match."));
      return;
    }
    pal.items.forEach((s, i) => {
      const row = el("div", "sv-pal-item" + (i === pal.sel ? " sel" : ""));
      row.appendChild(el("span", "sv-kind", s.kind || ""));
      row.appendChild(el("span", "sv-goal", s.name || ""));
      row.appendChild(el("span", "sv-pal-path", `${s.path}:${s.line}`));
      row.onclick = () => pickSymbol(i);
      list.appendChild(row);
    });
  }
  async function searchSymbols(q) {
    const list = document.getElementById("sv-pal-list");
    const term = String(q || "").trim();
    if (!term) {
      pal.items = [];
      pal.sel = 0;
      paintPalette("Type a symbol name.");
      return;
    }
    const seq = ++pal.seq;
    try {
      const r = await api("GET", "/api/v1/workspace/symbols?q=" + encodeURIComponent(term));
      if (seq !== pal.seq) return;
      pal.items = r && r.symbols || [];
      pal.sel = 0;
      if (r && r.truncated) {
        paintPalette(`Showing the first ${pal.items.length} matches (the search was cut short).`);
        return;
      }
      paintPalette();
    } catch (e) {
      if (seq !== pal.seq) return;
      pal.items = [];
      paintPalette("Symbol search failed: " + (e && e.message ? e.message : "unknown error"));
    }
  }
  async function pickSymbol(i) {
    const s = pal.items[i];
    if (!s) return;
    closePalette();
    try {
      await window.codeStudio?.openFile?.(s.path);
      const f = await api("GET", "/api/v1/workspace/file?path=" + encodeURIComponent(s.path));
      const ta = $3("edContent");
      if (ta && f && typeof f.content === "string") {
        const lines = f.content.split("\n");
        let offset = 0;
        for (let n = 0; n < Math.min(s.line - 1, lines.length); n++) offset += lines[n].length + 1;
        ta.focus();
        ta.setSelectionRange(offset, offset);
        const lh = parseFloat(getComputedStyle(ta).lineHeight) || 18;
        ta.scrollTop = Math.max(0, (s.line - 4) * lh);
      }
    } catch {
    }
  }
  function openPalette() {
    injectStyles();
    buildPalette();
    const wrap = document.getElementById("sv-pal");
    const input = document.getElementById("sv-pal-input");
    if (!wrap || !input) return;
    wrap.hidden = false;
    pal.open = true;
    pal.items = [];
    pal.sel = 0;
    input.value = "";
    paintPalette("Type a symbol name.");
    input.focus();
  }
  function closePalette() {
    const wrap = document.getElementById("sv-pal");
    if (wrap) wrap.hidden = true;
    pal.open = false;
  }
  function bootSupervision() {
    injectStyles();
    buildCard();
    buildPalette();
    refresh();
    setInterval(() => {
      if (!document.hidden) refresh();
    }, 4e3);
    document.addEventListener("keydown", (ev) => {
      if ((ev.metaKey || ev.ctrlKey) && ev.shiftKey && (ev.key === "O" || ev.key === "o")) {
        ev.preventDefault();
        if (pal.open) closePalette();
        else openPalette();
      } else if (ev.key === "Escape" && pal.open) {
        closePalette();
      }
    });
    window.supervision = { openPalette, closePalette, refresh };
  }

  // src/main.js
  var $4 = (id) => document.getElementById(id);
  var roster = { bots: [], skills: [], routines: [], messages: [], routes: [], runs: [] };
  var activeRun = null;
  var pollTimer = null;
  var elapsedTimer = null;
  var runStart = null;
  var streamES = null;
  var streamBuf = "";
  var streamOpen = false;
  var streamRetries = 0;
  var streamTimer = null;
  var streamRunId = null;
  var keyFile = "~/.accounts/meta.ai";
  var secretPatterns = [
    /MODEL_API_KEY\s*[=:]\s*\S+/g,
    /sk-[A-Za-z0-9_\-]{8,}/g,
    /Bearer\s+[A-Za-z0-9_\-\.]{8,}/g,
    new RegExp(keyFile.replace(/[.*+?^${}()|[\]\\/]/g, "\\$&"), "g"),
    /\/\.accounts\/meta\.ai/g
  ];
  function sanitize2(s) {
    let out = String(s ?? "");
    for (const re of secretPatterns) out = out.replace(re, "[REDACTED]");
    return out;
  }
  function esc3(s) {
    return String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
  }
  function safe2(s) {
    return esc3(sanitize2(s));
  }
  function toast3(msg, cls) {
    const t = document.createElement("div");
    t.className = "toast" + (cls ? " " + cls : "");
    t.textContent = msg;
    $4("toasts").appendChild(t);
    setTimeout(() => {
      t.style.opacity = "0";
      t.style.transition = "opacity .3s";
    }, 3200);
    setTimeout(() => t.remove(), 3600);
  }
  function showErr(e) {
    if (!e) return;
    toast3(e, "err");
  }
  async function api2(method, path, body) {
    const opt = { method, headers: {} };
    if (body !== void 0) {
      opt.headers["Content-Type"] = "application/json";
      opt.body = JSON.stringify(body);
    }
    const r = await fetch(path, opt);
    const text = await r.text();
    if (!r.ok) throw new Error(text.trim() || r.statusText);
    if (!text) return null;
    try {
      return JSON.parse(text);
    } catch {
      return text;
    }
  }
  function fillSelect(el2, items, label) {
    const cur = el2.value;
    el2.innerHTML = items.map((b) => `<option value="${esc3(b.id)}">${esc3(label(b))}</option>`).join("");
    if ([...el2.options].some((o) => o.value === cur)) el2.value = cur;
  }
  function fillRouteSelect(el2, mesh) {
    const cur = el2.value;
    const opts = ["glm", "qwen", "muse", "local"].map((id) => `<option value="${id}">${id}</option>`).join("");
    const meshOpts = (mesh || []).map((m) => `<option value="${esc3(m.route)}">${esc3(m.route)}${m.display ? " \xB7 " + esc3(m.display) : ""}</option>`).join("");
    el2.innerHTML = opts + meshOpts;
    if ([...el2.options].some((o) => o.value === cur)) el2.value = cur;
  }
  var nav = $4("nav");
  nav.querySelectorAll("button[data-panel]").forEach((btn) => {
    btn.onclick = () => {
      nav.querySelectorAll("button").forEach((b) => b.classList.remove("active"));
      btn.classList.add("active");
      document.querySelectorAll(".panel").forEach((p2) => p2.classList.remove("active"));
      const p = $4("panel-" + btn.dataset.panel);
      if (p) p.classList.add("active");
    };
  });
  function gotoPanel(name) {
    const btn = nav.querySelector(`button[data-panel="${name}"]`);
    if (btn) btn.click();
  }
  var meshModels = [];
  function avatarGrad(s) {
    let h = 0;
    for (let i = 0; i < s.length; i++) h = h * 31 + s.charCodeAt(i) >>> 0;
    const a = h % 360, b = (a + 40 + h % 60) % 360;
    return `linear-gradient(135deg, hsl(${a} 60% 60%), hsl(${b} 65% 52%))`;
  }
  function avatar(name) {
    const init = (name || "?").trim().slice(0, 2).toUpperCase();
    return `<span class="avatar" style="background:${avatarGrad(name)}">${esc3(init)}</span>`;
  }
  function empty(icon, text) {
    return `<li class="empty"><span class="ico">${icon}</span>${esc3(text)}</li>`;
  }
  function render2() {
    $4("navBots").textContent = roster.bots.length;
    $4("navRuns").textContent = (roster.runs || []).length;
    $4("navRtn").textContent = roster.routines.length;
    $4("navSkills").textContent = roster.skills.length;
    $4("statBots").textContent = roster.bots.length;
    $4("statRoutines").textContent = roster.routines.length;
    $4("statRuns").textContent = (roster.runs || []).length;
    $4("statMesh").textContent = meshModels.length;
    $4("bots").innerHTML = roster.bots.map(
      (b) => `<li>${avatar(b.name)}
      <div class="body">
        <div class="title">${esc3(b.name)} <span class="chip acc">${esc3(b.role)}</span></div>
        <div class="clip">${esc3(b.route)}${b.memory ? " \xB7 remembers: " + safe2(b.memory) : ""}</div>
      </div>
      <div class="actions">${presenceHTML(b)}<span class="tag">${esc3(b.id)}</span></div></li>`
    ).join("") || empty("\u25CD", "No teammates yet \u2014 deploy one above.");
    fillSelect($4("runBot"), roster.bots, (b) => `${b.name} (${b.route})`);
    window.codeStudio?.fillRunBot?.(roster.bots);
    fillSelect($4("rtnBot"), roster.bots, (b) => b.name);
    fillSelect($4("fromBot"), roster.bots, (b) => b.name);
    fillSelect($4("toBot"), roster.bots, (b) => b.name);
    fillSelect($4("rtnSkill"), roster.skills, (s) => s.name);
    fillSelect($4("memBot"), roster.bots, (b) => b.name);
    fillRouteSelect($4("botRoute"), meshModels);
    $4("handoff").style.display = roster.bots.length >= 2 ? "block" : "none";
    const botName = {};
    for (const b of roster.bots) botName[b.id] = b.name;
    $4("msgs").innerHTML = (roster.messages || []).map(
      (m) => `<li>${avatar(m.from)}
      <div class="body">
        <div class="title">${esc3(botName[m.from] || m.from)} \u2192 ${esc3(botName[m.to] || m.to)} <span class="tag">owner ${esc3(botName[m.owner_bot] || m.owner_bot)}</span></div>
        <div class="clip">${safe2(m.body)}</div>
      </div></li>`
    ).join("") || empty("\u21C4", "No handoffs yet.");
    $4("skills").innerHTML = roster.skills.map(
      (s) => `<li><span class="avatar" style="background:var(--grad)">\u2726</span>
      <div class="body"><div class="title">${esc3(s.name)}</div><div class="clip">${esc3(s.id)}</div></div></li>`
    ).join("") || empty("\u2726", "The library is empty \u2014 write a playbook above.");
    $4("routines").innerHTML = roster.routines.map((rt) => {
      const state2 = rt.paused ? `<span class="chip warn">paused</span>` : `<span class="chip ok">on</span>`;
      const next = rt.next_fire ? ` \xB7 next ${esc3(rt.next_fire)}` : "";
      return `<li><span class="avatar" style="background:var(--grad)">\u21BB</span>
      <div class="body">
        <div class="title">${esc3(rt.trigger)} ${state2}</div>
        <div class="clip">${esc3(rt.id)} \xB7 ${esc3(rt.last_note || "no test-run")}${next}</div>
        <div class="rtn-edit" data-rtn-edit="${esc3(rt.id)}" hidden>
          <input class="rtn-edit-in" value="${esc3(rt.trigger)}" aria-label="trigger" spellcheck="false">
          <button class="ghost mini" data-rtn-save="${esc3(rt.id)}">save trigger</button>
        </div>
      </div>
      <div class="actions">
        <button class="ghost" data-test="${esc3(rt.id)}">test-run</button>
        <button class="ghost" data-en="${esc3(rt.id)}">enable</button>
        <button class="ghost" data-pause="${esc3(rt.id)}" data-paused="${rt.paused ? "0" : "1"}">${rt.paused ? "resume" : "pause"}</button>
        <button class="ghost" data-rtn-edit-btn="${esc3(rt.id)}">trigger</button>
      </div></li>`;
    }).join("") || empty("\u21BB", "Nothing on the clock \u2014 arm a routine above.");
    $4("routines").querySelectorAll("[data-test]").forEach((btn) => btn.onclick = () => testRun(btn.dataset.test));
    $4("routines").querySelectorAll("[data-en]").forEach((btn) => btn.onclick = () => enableRtn(btn.dataset.en));
    $4("routines").querySelectorAll("[data-pause]").forEach((btn) => btn.onclick = () => pauseRtn(btn.dataset.pause, btn.dataset.paused === "1"));
    $4("routines").querySelectorAll("[data-rtn-edit-btn]").forEach((btn) => btn.onclick = () => {
      const box = $4("routines").querySelector(`[data-rtn-edit="${btn.dataset.rtnEditBtn}"]`);
      if (box) {
        box.hidden = !box.hidden;
        box.querySelector("input")?.focus();
      }
    });
    $4("routines").querySelectorAll("[data-rtn-save]").forEach((btn) => btn.onclick = () => saveRtnTrigger(btn.dataset.rtnSave));
    const tb = $4("routes").querySelector("tbody");
    tb.innerHTML = (roster.routes || []).map(
      (r) => `<tr><td>${esc3(r.id)}</td><td>${esc3(r.role)}</td><td>${esc3(r.model)}</td><td class="tag">${esc3(r.purpose)}</td></tr>`
    ).join("");
    const fmtTime = (ts) => {
      if (!ts) return "";
      const d = new Date(ts);
      return isNaN(d) ? "" : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    };
    $4("runHistory").innerHTML = (roster.runs || []).slice(0, 20).map((r) => {
      const chip = r.status === "done" ? "ok" : r.status === "running" ? "acc" : "err";
      return `<li>
      <div class="body">
        <div class="title"><button class="linklike" data-run="${esc3(r.id)}">${esc3(r.id.slice(0, 8))}</button> <span class="chip ${chip}">${esc3(r.status)}</span>${r.created_at ? ` <span class="tag">${esc3(fmtTime(r.created_at))}</span>` : ""}</div>
        <div class="clip">${esc3(botName[r.bot_id] || r.bot_id || "\u2014")} \xB7 ${esc3(r.intent)}${r.error ? " \xB7 " + esc3(r.error) : ""}</div>
      </div></li>`;
    }).join("") || empty("\u25B6", "Nothing has run yet. Give it a mission.");
    $4("runHistory").querySelectorAll("[data-run]").forEach((btn) => btn.onclick = () => {
      clearLog();
      stopPoll();
      activeRun = null;
      startStream(btn.dataset.run);
    });
  }
  async function refresh2() {
    roster = await api2("GET", "/api/v1/roster");
    render2();
    renderSpark();
    renderHero();
  }
  async function saveRtnTrigger(id) {
    const box = $4("routines").querySelector(`[data-rtn-edit="${id}"]`);
    const input = box?.querySelector(".rtn-edit-in");
    const trigger = (input?.value || "").trim();
    if (!trigger) {
      toast3("a trigger is required", "err");
      return;
    }
    try {
      const res = await api2("PATCH", `/api/v1/routines/${encodeURIComponent(id)}`, { trigger });
      toast3("trigger updated \u2014 next fire " + (res?.next_fire || "not scheduled"), "ok");
      if (box) box.hidden = true;
      await refresh2();
    } catch (e) {
      toast3(safe2(e.message), "err");
    }
  }
  async function loadMesh() {
    try {
      const res = await api2("GET", "/api/v1/mesh/models");
      meshModels = res.ok ? res.models || [] : [];
      $4("meshCount").textContent = res.ok ? `${meshModels.length} ready` : `unavailable (${res.reason || "amesh-web unreachable"})`;
    } catch (e) {
      meshModels = [];
      $4("meshCount").textContent = `unavailable (${e.message})`;
    }
    $4("meshModels").innerHTML = meshModels.map(
      (m) => `<li><span class="avatar" style="background:var(--grad)">\u25C8</span>
      <div class="body">
        <div class="title">${esc3(m.route)}</div>
        <div class="clip">${esc3(m.display || m.model)}${m.context_tokens ? " \xB7 " + m.context_tokens + " ctx" : ""}</div>
      </div></li>`
    ).join("") || empty("\u25C8", "No mesh models ready \u2014 the rack is dark.");
    render2();
  }
  var hubData = { hubs: [], models: [] };
  async function loadHubs() {
    const statusEl = $4("hubsStatus");
    const listEl = $4("hubList");
    const modelsEl = $4("hubModels");
    const countEl = $4("modelCount");
    const navEl = $4("navHubs");
    try {
      const res = await api2("GET", "/api/v1/hubs");
      hubData.hubs = res.hubs || [];
      hubData.models = res.models || [];
      if (statusEl) statusEl.textContent = res.ok ? `${hubData.hubs.length} hub${hubData.hubs.length === 1 ? "" : "s"} joined` : `unavailable (${res.hub_error || res.model_error || "amesh unreachable"})`;
      if (navEl) navEl.textContent = String(hubData.hubs.length);
      if (countEl) countEl.textContent = String(hubData.models.length);
    } catch (e) {
      hubData.hubs = [];
      hubData.models = [];
      if (statusEl) statusEl.textContent = `unavailable (${e.message})`;
      if (navEl) navEl.textContent = "0";
      if (countEl) countEl.textContent = "0";
    }
    if (listEl) {
      listEl.innerHTML = hubData.hubs.map((h) => {
        const wg = h.wg_up ? `<span class="chip ok">wg up</span>` : `<span class="chip err">wg down</span>`;
        const def = h.default ? `<span class="chip acc">default</span>` : "";
        const ver = h.hub_version ? `<span class="tag">${esc3(h.hub_version)}</span>` : "";
        return `<div class="hub-card" data-name="${esc3(h.name)}" title="inspect ${esc3(h.name)}">
        <div class="hub-title"><span class="avatar" style="background:var(--grad)">\u25C8</span><span class="hub-name">${esc3(h.name)}</span>${def}${wg}${ver}</div>
        <div class="hub-url">${esc3(h.url)}</div>
        <div class="hub-meta">${h.controller_url ? `controller ${esc3(h.controller_url)}` : ""}${h.wg_status && h.wg_status.ip ? ` \xB7 wg ${esc3(h.wg_status.ip)}` : ""}<span class="hub-drill tag">inspect \u203A</span></div>
      </div>`;
      }).join("") || empty("\u25C8", "No hubs joined \u2014 run 'amesh join <hub-url>' to enrol.");
      listEl.querySelectorAll(".hub-card").forEach((card) => {
        card.onclick = () => openHubDetail(card.dataset.name);
      });
    }
    if (modelsEl) {
      modelsEl.innerHTML = hubData.models.map((m) => {
        const st = m.state === "ready" ? `<span class="chip ok">ready</span>` : `<span class="chip">stopped</span>`;
        const loc = m.location === "local" ? `<span class="tag">local</span>` : `<span class="tag">${esc3(m.location)}</span>`;
        const backend = m.backend ? `<span class="tag">${esc3(m.backend)}</span>` : "";
        const shared = m.shared ? `<span class="chip acc" title="shared on the mesh">\u25C8 shared</span>` : "";
        const routes = m.route_count > 0 ? `<span class="tag">${m.route_count} route${m.route_count === 1 ? "" : "s"}</span>` : "";
        const startBtn = m.state !== "ready" ? `<button type="button" class="ghost mini" data-start="${esc3(m.id)}" title="warm ${esc3(m.display)}">\u25B6 start</button>` : "";
        return `<div class="hub-model" data-state="${esc3(m.state)}">
        <div class="hub-model-name">${esc3(m.display || m.id)}</div>
        <div class="hub-model-meta">${loc}${st}${backend}${shared}${routes}${m.context_tokens ? ` \xB7 ${m.context_tokens} ctx` : ""}</div>
        <div class="spacer"></div>${startBtn}
      </div>`;
      }).join("") || empty("\u25C8", "No model services found.");
      modelsEl.querySelectorAll("[data-start]").forEach((btn) => {
        btn.onclick = () => startModel(btn.dataset.start, btn);
      });
    }
  }
  async function startModel(id, btn) {
    if (btn) {
      btn.disabled = true;
      btn.textContent = "starting\u2026";
    }
    const row = btn && btn.closest(".hub-model");
    if (row) row.dataset.state = "starting";
    try {
      await api2("POST", "/api/v1/models/" + encodeURIComponent(id) + "/start");
      toast3("start accepted \u2014 model warming up", "ok");
      setTimeout(() => loadHubs(), 4e3);
    } catch (e) {
      toast3(safe2(e.message), "err");
      if (btn) {
        btn.disabled = false;
        btn.textContent = "\u25B6 start";
      }
      if (row) row.dataset.state = "stopped";
    }
  }
  async function runDoctor() {
    const bar = $4("hubDoctor");
    if (!bar) return;
    bar.hidden = false;
    bar.innerHTML = `<span class="tag">running amesh doctor\u2026</span>`;
    try {
      const d = await api2("GET", "/api/v1/hubs/doctor");
      const checks = d.checks || [];
      const byStatus = (s) => checks.filter((c) => c.status === s).length;
      const ok = byStatus("ok"), warn = byStatus("warning") + byStatus("advisory"), err = byStatus("error");
      const chipCls = err ? "err" : warn ? "warn" : "ok";
      bar.innerHTML = `<div class="hub-doctor-summary">
        <span class="chip ${chipCls}">mesh ${d.ok ? "healthy" : "attention"}</span>
        <span class="tag">${ok} ok</span>
        <span class="tag">${warn} warn</span>
        <span class="tag">${err} err</span>
        <span class="tag">default hub ${esc3(d.default_hub || "\u2014")}</span>
      </div>
      <div class="hub-doctor-checks">${checks.map(
        (c) => `<div class="hub-check ${c.status}"><span class="chip ${c.status === "ok" ? "ok" : c.status === "error" ? "err" : "warn"}">${esc3(c.status)}</span><span class="hub-check-id">${esc3(c.id)}</span><span class="hub-check-msg">${esc3(c.message)}</span></div>`
      ).join("") || `<span class="tag">no checks</span>`}</div>`;
    } catch (e) {
      bar.innerHTML = `<span class="tag err">doctor failed: ${safe2(e.message)}</span>`;
    }
  }
  async function openHubDetail(name) {
    const el2 = $4("hubDetail");
    if (!el2) return;
    el2.hidden = false;
    $4("hubDetailName").textContent = name;
    $4("hubDetailBody").innerHTML = `<span class="tag">inspecting ${esc3(name)}\u2026</span>`;
    try {
      const d = await api2("GET", "/api/v1/hubs/" + encodeURIComponent(name));
      const h = d.hub || {};
      const checks = d.checks || [];
      const wg = h.wg_up ? `<span class="chip ok">wg up</span>` : `<span class="chip err">wg down</span>`;
      const def = h.default ? `<span class="chip acc">default</span>` : "";
      $4("hubDetailBody").innerHTML = `
      <div class="hub-detail-row"><span class="tag">url</span><code>${esc3(h.url || "")}</code></div>
      ${h.controller_url ? `<div class="hub-detail-row"><span class="tag">controller</span><code>${esc3(h.controller_url)}</code></div>` : ""}
      ${h.fingerprint ? `<div class="hub-detail-row"><span class="tag">fingerprint</span><code>${esc3(h.fingerprint)}</code></div>` : ""}
      ${h.wg_status && h.wg_status.ip ? `<div class="hub-detail-row"><span class="tag">wg ip</span><code>${esc3(h.wg_status.ip)}</code></div>` : ""}
      <div class="hub-detail-row"><span class="tag">status</span>${wg}${def}</div>
      <h4>Health checks</h4>
      ${checks.map((c) => `<div class="hub-check ${c.status}"><span class="chip ${c.status === "ok" ? "ok" : c.status === "error" ? "err" : "warn"}">${esc3(c.status)}</span><span class="hub-check-id">${esc3(c.id)}</span><span class="hub-check-msg">${esc3(c.message)}</span></div>`).join("") || `<span class="tag">no health checks for this hub</span>`}
    `;
    } catch (e) {
      $4("hubDetailBody").innerHTML = `<span class="tag err">${safe2(e.message)}</span>`;
    }
  }
  var openTools = /* @__PURE__ */ new Map();
  function logEl() {
    return $4("log");
  }
  function appendEvent(e) {
    const log = logEl();
    const kind = e.kind || "";
    stats.bump(kind);
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
        permPendingBot = null;
        break;
      case "tool_end":
        node = toolEl(e, true);
        if (isDiff(e.text)) node.appendChild(htmlNode(renderDiff2(e.text)));
        checkStep();
        break;
      case "perm":
        node = permEl(e);
        permPendingBot = botOfActiveRun();
        break;
      case "plan":
        node = planEl(e.text);
        break;
      case "err":
        node = evDiv("err", "err", e.text);
        sfx.fail();
        break;
      case "done":
        node = evDiv("done", "done", e.text);
        sfx.done();
        renderTimeline();
        checkAllSteps();
        permPendingBot = null;
        onRunEnd();
        break;
      case "verify":
        node = verifyEl(e);
        if (!e.is_error) confetti();
        else sfx.fail();
        break;
      case "run_status":
        node = evDiv("run_status", "run_status", e.text);
        break;
      case "routine_fire":
        node = evDiv("routine_fire", "routine_fire", "routine " + e.text + " fired");
        break;
      default:
        node = evDiv("unknown", kind, e.text);
    }
    log.appendChild(node);
    stampEvent(node, kind);
    if (autoScroll) log.scrollTop = log.scrollHeight;
  }
  function evDiv(cls, label, text) {
    const d = document.createElement("div");
    d.className = "ev " + cls;
    const k = document.createElement("span");
    k.className = "k";
    k.textContent = label;
    d.appendChild(k);
    if (text) d.appendChild(document.createTextNode(" " + sanitize2(text)));
    return d;
  }
  function thinkingEl(text) {
    const d = document.createElement("details");
    d.className = "ev thinking";
    const s = document.createElement("summary");
    s.className = "k";
    s.textContent = "thinking";
    d.appendChild(s);
    const body = document.createElement("div");
    body.className = "body";
    body.textContent = sanitize2(text || "");
    d.appendChild(body);
    return d;
  }
  var planSteps = null;
  function planEl(text) {
    const d = document.createElement("div");
    d.className = "ev plan";
    const k = document.createElement("span");
    k.className = "k";
    k.textContent = "plan";
    d.appendChild(k);
    const ol = document.createElement("ol");
    ol.className = "plan-steps";
    for (const raw of String(text || "").split("\n")) {
      const step = raw.replace(/^\s*(?:\d+\.|[-*•])\s*/, "").trim();
      if (!step) continue;
      const li = document.createElement("li");
      li.className = "pstep";
      li.textContent = step;
      ol.appendChild(li);
    }
    if (ol.children.length) {
      d.appendChild(ol);
      planSteps = ol;
    } else if (text) d.appendChild(document.createTextNode(" " + sanitize2(text)));
    return d;
  }
  function checkStep() {
    if (!planSteps) return;
    const next = [...planSteps.children].find((li) => !li.classList.contains("done"));
    if (next) next.classList.add("done");
  }
  function checkAllSteps() {
    if (!planSteps) return;
    [...planSteps.children].forEach((li) => li.classList.add("done"));
  }
  var permPendingBot = null;
  function botOfActiveRun() {
    return ((roster.runs || []).find((r) => r.id === streamRunId) || {}).bot_id || null;
  }
  function presenceHTML(b) {
    if (permPendingBot === b.id)
      return `<span class="presence warn"><span class="pdot"></span>waiting on you</span>`;
    const working = (roster.runs || []).some((r) => r.bot_id === b.id && r.status === "running");
    return working ? `<span class="presence acc"><span class="pdot"></span>working</span>` : `<span class="presence"><span class="pdot"></span>idle</span>`;
  }
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
      const open = openTools.get(tool);
      if (open) {
        const st = open.querySelector(".tool-status");
        if (st) st.textContent = e.is_error ? " failed" : " done";
        open.classList.add("closed");
        if (e.is_error) open.classList.add("err");
        openTools.delete(tool);
      }
      if (e.is_error) d.classList.add("err");
      if (e.text) d.appendChild(document.createTextNode(" " + sanitize2(e.text)));
    }
    return d;
  }
  function permEl(e) {
    const d = document.createElement("div");
    d.className = "ev perm";
    const k = document.createElement("span");
    k.className = "k";
    k.textContent = "perm";
    d.appendChild(k);
    if (e.text) d.appendChild(document.createTextNode(" " + sanitize2(e.text)));
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
  function verifyEl(e) {
    const d = document.createElement("div");
    const ok = !e.is_error;
    d.className = "ev verify " + (ok ? "ok" : "fail");
    const badge = document.createElement("span");
    badge.className = "badge";
    badge.textContent = ok ? "verify \u2713" : "verify \u2717";
    d.appendChild(badge);
    if (e.text) d.appendChild(document.createTextNode(" " + sanitize2(e.text)));
    return d;
  }
  function onRunEnd() {
    stopElapsed();
    refresh2().catch(() => {
    });
  }
  function clearLog() {
    logEl().innerHTML = "";
    openTools.clear();
    evCount = 0;
    updateStats();
    planSteps = null;
    $4("timeline").hidden = true;
  }
  function startElapsed() {
    runStart = Date.now();
    stopElapsed();
    const tick = () => {
      if (!runStart) return;
      const s = Math.floor((Date.now() - runStart) / 1e3);
      const el2 = $4("elapsed");
      if (el2) el2.textContent = s < 60 ? s + "s" : Math.floor(s / 60) + "m " + s % 60 + "s";
    };
    tick();
    elapsedTimer = setInterval(tick, 1e3);
  }
  function stopElapsed() {
    if (elapsedTimer) {
      clearInterval(elapsedTimer);
      elapsedTimer = null;
    }
  }
  function handleEvent(line) {
    line = line.trim();
    if (!line) return;
    let ev;
    try {
      ev = JSON.parse(line);
    } catch {
      return;
    }
    if (!ev || typeof ev !== "object") return;
    appendEvent(ev);
  }
  function feedStreamLine(chunk) {
    streamBuf += chunk;
    let idx;
    while ((idx = streamBuf.indexOf("\n")) >= 0) {
      const line = streamBuf.slice(0, idx);
      streamBuf = streamBuf.slice(idx + 1);
      handleEvent(line);
    }
  }
  function connectStream() {
    if (streamES) {
      try {
        streamES.abort();
      } catch {
      }
      streamES = null;
    }
    if (streamTimer) {
      clearTimeout(streamTimer);
      streamTimer = null;
    }
    const url = streamRunId ? "/api/stream?run=" + encodeURIComponent(streamRunId) : "/api/stream";
    const ctrl = new AbortController();
    streamES = ctrl;
    fetch(url, { signal: ctrl.signal }).then((res) => {
      if (!res.ok) throw new Error("stream " + res.status);
      streamOpen = true;
      streamRetries = 0;
      setStreamState("live");
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      const pump = () => reader.read().then(({ done, value }) => {
        if (done) throw new Error("eof");
        streamBuf += dec.decode(value, { stream: true });
        let idx;
        while ((idx = streamBuf.indexOf("\n")) >= 0) {
          const line = streamBuf.slice(0, idx);
          streamBuf = streamBuf.slice(idx + 1);
          feedStreamLine(line);
        }
        return pump();
      });
      return pump();
    }).catch((e) => {
      if (e.name === "AbortError") return;
      streamOpen = false;
      setStreamState("reconnecting");
      streamES = null;
      streamRetries++;
      const delay = Math.min(1e3 * 2 ** streamRetries, 15e3);
      streamTimer = setTimeout(connectStream, delay);
    });
  }
  function setStreamState(state2) {
    const el2 = $4("streamState");
    const pill = $4("streamPill");
    if (el2) el2.textContent = state2;
    if (pill) {
      pill.classList.remove("live", "reconnecting");
      if (state2 === "live" || state2 === "reconnecting") pill.classList.add(state2);
    }
  }
  function startStream(runId) {
    streamRunId = runId || null;
    if (streamTimer) {
      clearTimeout(streamTimer);
      streamTimer = null;
    }
    streamBuf = "";
    connectStream();
    if (runId) startElapsed();
    else {
      runStart = null;
      stopElapsed();
    }
  }
  function stopStream() {
    if (streamTimer) {
      clearTimeout(streamTimer);
      streamTimer = null;
    }
    if (streamES) {
      try {
        streamES.abort();
      } catch {
      }
      streamES = null;
    }
    streamOpen = false;
    streamRunId = null;
    setStreamState("idle");
  }
  function stopPoll() {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }
  function renderMissions() {
    $4("missionGrid").innerHTML = MISSIONS.map((m, i) => `<button type="button" class="mission" data-mission="${i}">
       <span class="m-ico">${m.icon}</span>
       <span class="m-title">${esc3(m.title)}</span>
       <span class="m-desc">${esc3(m.prompt)}</span>
     </button>`).join("");
    $4("missionGrid").querySelectorAll("[data-mission]").forEach((btn) => {
      btn.onclick = () => {
        const m = MISSIONS[+btn.dataset.mission];
        $4("prompt").value = m.prompt;
        $4("intent").value = m.intent;
        $4("prompt").focus();
        toast3(`mission loaded: ${m.title}`, "ok");
      };
    });
  }
  function renderSkillTemplates() {
    $4("skillTemplates").innerHTML = SKILL_TEMPLATES.map((t, i) => `<button type="button" class="mission" data-tpl="${i}">
       <span class="m-ico">\u2726</span>
       <span class="m-title">${esc3(t.name)}</span>
       <span class="m-desc">${esc3(t.body.split("\n").slice(1).join(" ").trim().slice(0, 120))}\u2026</span>
     </button>`).join("");
    $4("skillTemplates").querySelectorAll("[data-tpl]").forEach((btn) => {
      btn.onclick = () => {
        const t = SKILL_TEMPLATES[+btn.dataset.tpl];
        $4("skillName").value = t.name;
        $4("skillBody").value = t.body;
        toast3(`template loaded: ${t.name}`, "ok");
      };
    });
  }
  function renderRtnTriggers() {
    $4("rtnTriggers").innerHTML = ROUTINE_TRIGGERS.map((t) => `<button type="button" class="chip ghost-chip" data-trig="${esc3(t.value)}">${esc3(t.label)}</button>`).join("");
    $4("rtnTriggers").querySelectorAll("[data-trig]").forEach((btn) => {
      btn.onclick = () => {
        $4("rtnTrigger").value = btn.dataset.trig;
      };
    });
  }
  var stats = new Stats();
  var sfx = new Sfx();
  function htmlNode(html) {
    const t = document.createElement("template");
    t.innerHTML = html;
    return t.content.firstElementChild;
  }
  setInterval(() => {
    $4("tsEpm").textContent = stats.perMin();
    $4("tsTools").textContent = stats.tools();
    $4("tsErrs").textContent = stats.errs();
    $4("tsTotal").textContent = stats.total();
  }, 1e3);
  function renderTimeline() {
    const events = [...logEl().querySelectorAll(".ev")].map((r) => ({ kind: r.dataset.kind }));
    const segs = timeline(events);
    const tl = $4("timeline");
    if (!segs.length) {
      tl.hidden = true;
      return;
    }
    tl.hidden = false;
    tl.innerHTML = segs.map((s) => `<div class="tl-seg tl-${s.p}" style="flex:${Math.max(s.frac * 100, 4)}" title="${Math.round(s.frac * 100)}% ${s.p}"></div>`).join("");
  }
  $4("sfxToggle").onclick = () => {
    sfx.on = !sfx.on;
    $4("sfxToggle").textContent = sfx.on ? "\u266A on" : "\u266A off";
    $4("sfxToggle").classList.toggle("sel", sfx.on);
    if (sfx.on) sfx.tick();
  };
  $4("shareRun").onclick = async () => {
    const events = [...logEl().querySelectorAll(".ev")].map((r) => {
      const ev = { kind: r.dataset.kind };
      const k = r.querySelector(".k");
      if (k) ev.tool = k.textContent.replace(/^tool_(start|end)\s*/, "");
      const body = r.querySelector(".body") || r.querySelector(".diff");
      ev.text = body ? body.textContent : r.textContent.replace(/^[a-z_]+\s*/, "");
      return ev;
    });
    const run = (roster.runs || []).find((r) => r.id === streamRunId) || { id: streamRunId || "live", bot_id: "", intent: "", status: "live" };
    const botName = (roster.bots.find((b) => b.id === run.bot_id) || {}).name;
    try {
      await navigator.clipboard.writeText(toMarkdown(run, events, botName));
      toast3("transcript copied as markdown", "ok");
    } catch {
      toast3("clipboard blocked", "err");
    }
  };
  var logFilter = "all";
  var showTs = false;
  var autoScroll = true;
  var evCount = 0;
  function tsFor() {
    const d = /* @__PURE__ */ new Date();
    const p = (n) => String(n).padStart(2, "0");
    return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
  }
  function applyFilter() {
    const log = logEl();
    log.classList.toggle("show-ts", showTs);
    log.querySelectorAll(".ev").forEach((row) => {
      const k = row.dataset.kind || "";
      const show = logFilter === "all" || (logFilter === "tool" ? k === "tool_start" || k === "tool_end" : k === logFilter);
      row.style.display = show ? "" : "none";
    });
    updateStats();
  }
  function updateStats() {
    const log = logEl();
    const visible = [...log.querySelectorAll(".ev")].filter((r) => r.style.display !== "none").length;
    $4("logStats").textContent = `${visible} events`;
  }
  $4("logFilters").querySelectorAll("[data-f]").forEach((btn) => {
    btn.onclick = () => {
      $4("logFilters").querySelectorAll("[data-f]").forEach((b) => b.classList.remove("sel"));
      btn.classList.add("sel");
      logFilter = btn.dataset.f;
      applyFilter();
    };
  });
  $4("tsToggle").onclick = () => {
    showTs = !showTs;
    $4("tsToggle").classList.toggle("sel", showTs);
    applyFilter();
  };
  $4("autoScroll").onclick = () => {
    autoScroll = !autoScroll;
    $4("autoScroll").classList.toggle("sel", autoScroll);
    if (autoScroll) logEl().scrollTop = logEl().scrollHeight;
  };
  $4("copyLog").onclick = async () => {
    const lines = [...logEl().querySelectorAll(".ev")].map((r) => r.textContent.trim()).join("\n");
    try {
      await navigator.clipboard.writeText(lines);
      toast3("console copied", "ok");
    } catch {
      toast3("clipboard blocked", "err");
    }
  };
  function stampEvent(node, kind) {
    node.dataset.kind = kind;
    const ts = document.createElement("span");
    ts.className = "ts";
    ts.textContent = tsFor();
    node.insertBefore(ts, node.firstChild);
    evCount++;
    updateStats();
  }
  function renderSpark() {
    const runs = roster.runs || [];
    $4("sparkCard").hidden = runs.length === 0;
    if (!runs.length) return;
    const now = Date.now();
    const buckets = new Array(24).fill(0);
    for (const r of runs) {
      const t = Date.parse(r.created_at);
      if (isNaN(t)) continue;
      const h = Math.floor((now - t) / 36e5);
      if (h >= 0 && h < 24) buckets[23 - h]++;
    }
    const max = Math.max(...buckets, 1);
    $4("spark").innerHTML = buckets.map((n, i) => `<div class="spark-col" title="${n} run${n === 1 ? "" : "s"}">
       <div class="spark-bar" style="height:${Math.max(4, n / max * 100)}%"></div>
       <div class="spark-lbl">${i === 23 ? "now" : i === 0 ? "-24h" : ""}</div>
     </div>`).join("");
  }
  function renderHero() {
    const empty2 = roster.bots.length === 0;
    $4("heroCard").hidden = !empty2;
    if (!empty2) return;
    $4("heroDeploy").onclick = () => gotoPanel("bots");
    $4("heroDemo").onclick = playDemoRun;
  }
  var palette = null;
  function paletteCommands() {
    const cmds = [
      { label: "Go to Overview", icon: "\u25CE", hint: "panel", run: () => gotoPanel("overview") },
      { label: "Go to Teammates", icon: "\u25CD", hint: "panel", run: () => gotoPanel("bots") },
      { label: "Go to Run", icon: "\u25B6", hint: "panel", run: () => gotoPanel("run") },
      { label: "Go to Code", icon: "\u276F", hint: "panel", run: () => gotoPanel("editor") },
      { label: "Open file\u2026", icon: "\u276F", hint: "editor", run: () => {
        gotoPanel("editor");
        window.codeStudio?.openQuick();
      } },
      { label: "Go to Routines", icon: "\u21BB", hint: "panel", run: () => gotoPanel("routines") },
      { label: "Go to Skills", icon: "\u2726", hint: "panel", run: () => gotoPanel("skills") },
      { label: "Go to Routes", icon: "\u21C4", hint: "panel", run: () => gotoPanel("routes") },
      { label: "Go to Model health", icon: "\u2665", hint: "panel", run: () => gotoPanel("health") },
      { label: "Watch a demo run", icon: "\u25B6", hint: "demo", run: playDemoRun },
      { label: "Run the pulse check", icon: "\u2665", hint: "health", run: () => {
        gotoPanel("health");
        $4("probeBtn").click();
      } },
      { label: "Copy the console", icon: "\u29C9", hint: "log", run: () => $4("copyLog").click() }
    ];
    for (const m of MISSIONS) {
      cmds.push({ label: `Mission: ${m.title}`, icon: m.icon, hint: m.intent, run: () => {
        gotoPanel("run");
        $4("prompt").value = m.prompt;
        $4("intent").value = m.intent;
        $4("prompt").focus();
        toast3(`mission loaded: ${m.title}`, "ok");
      } });
    }
    for (const b of roster.bots) {
      cmds.push({ label: `Run with ${b.name}`, icon: "\u25B6", hint: b.role, run: () => {
        gotoPanel("run");
        fillSelect($4("runBot"), roster.bots, (x) => `${x.name} (${x.route})`);
        $4("runBot").value = b.id;
        $4("prompt").focus();
      } });
    }
    for (const s of roster.skills) {
      cmds.push({ label: `Skill: ${s.name}`, icon: "\u2726", hint: "library", run: () => gotoPanel("skills") });
    }
    return cmds;
  }
  function openPalette2() {
    if (palette) {
      palette.close();
      return;
    }
    palette = new Palette({ commands: paletteCommands(), onClose: () => {
      palette = null;
    } });
  }
  var gPending = false;
  var gTimer = null;
  document.addEventListener("keydown", (e) => {
    const typing = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName || "");
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
      e.preventDefault();
      openPalette2();
      return;
    }
    if (typing) return;
    if (e.key === "g" && !gPending) {
      gPending = true;
      clearTimeout(gTimer);
      gTimer = setTimeout(() => {
        gPending = false;
      }, 800);
      return;
    }
    if (gPending) {
      const map = { o: "overview", b: "bots", r: "run", e: "editor", t: "routines", s: "skills", h: "health", m: "routes" };
      const panel = map[e.key];
      gPending = false;
      clearTimeout(gTimer);
      if (panel) {
        e.preventDefault();
        gotoPanel(panel);
      }
    }
  });
  var demoCancel = null;
  function playDemoRun() {
    if (demoCancel) {
      demoCancel();
      demoCancel = null;
    }
    stopStream();
    clearLog();
    gotoPanel("run");
    $4("runWho").textContent = "demo \xB7 scripted replay";
    startElapsed();
    demoCancel = playDemo(appendEvent, () => {
      demoCancel = null;
      stopElapsed();
      toast3("demo finished \u2014 now run a real one", "ok");
      startStream(null);
    });
  }
  $4("demoBtn").onclick = playDemoRun;
  function require2(id, msg) {
    const el2 = $4(id);
    const v = String(el2 ? el2.value : "").trim();
    if (!v) throw new Error(msg);
    return v;
  }
  function markField(id) {
    const el2 = $4(id);
    if (!el2) return;
    el2.classList.add("invalid");
    setTimeout(() => el2.classList.remove("invalid"), 1200);
  }
  $4("createBot").onclick = async () => {
    try {
      const name = require2("botName", "give the teammate a name");
      await api2("POST", "/api/v1/bots", { name, role: $4("botRole").value.trim() || "coder", route: $4("botRoute").value });
      toast3("teammate deployed", "ok");
      await refresh2();
    } catch (e) {
      showErr(e.message);
      markField("botName");
    }
  };
  $4("memBtn").onclick = async () => {
    try {
      const bot = require2("memBot", "deploy a teammate first");
      const note = require2("memNote", "what should it remember?");
      await api2("POST", "/api/v1/bots/" + bot + "/remember", { note });
      $4("memNote").value = "";
      toast3("burned into memory", "ok");
      await refresh2();
    } catch (e) {
      showErr(e.message);
      markField("memNote");
    }
  };
  $4("createSkill").onclick = async () => {
    try {
      const name = require2("skillName", "give the playbook a name");
      const body = require2("skillBody", "write the playbook body");
      await api2("POST", "/api/v1/skills", { name, body });
      toast3("playbook added to the library", "ok");
      await refresh2();
    } catch (e) {
      showErr(e.message);
      markField("skillName");
    }
  };
  $4("createRtn").onclick = async () => {
    try {
      const bot = require2("rtnBot", "deploy a teammate first");
      const skill = require2("rtnSkill", "add a playbook first");
      await api2("POST", "/api/v1/routines", { bot_id: bot, skill_id: skill, trigger: $4("rtnTrigger").value.trim() || "manual" });
      toast3("routine armed", "ok");
      await refresh2();
    } catch (e) {
      showErr(e.message);
    }
  };
  $4("sendHandoff").onclick = async () => {
    try {
      const from = require2("fromBot", "deploy a teammate first");
      const to = require2("toBot", "deploy a recipient teammate first");
      if (from === to) throw new Error("pick two different teammates");
      const body = require2("handoffBody", "what is the work item?");
      const out = await api2("POST", "/api/v1/handoff", { from, to, body, run: $4("handoffRun").checked });
      toast3("handoff away", "ok");
      await refresh2();
      if (out.run && out.run.id) {
        clearLog();
        stopPoll();
        activeRun = null;
        gotoPanel("run");
        startStream(out.run.id);
      }
    } catch (e) {
      showErr(e.message);
      markField("handoffBody");
    }
  };
  $4("runBtn").onclick = async () => {
    try {
      const bot = require2("runBot", "deploy a teammate first");
      const prompt = require2("prompt", "type a prompt for the run");
      const run = await api2("POST", "/api/v1/runs", {
        bot_id: bot,
        prompt,
        intent: $4("intent").value,
        yolo: $4("yolo").checked,
        verify: $4("verify").checked,
        worktree: $4("worktree").value
      });
      clearLog();
      stopPoll();
      activeRun = null;
      gotoPanel("run");
      sfx.start();
      startStream(run.id);
    } catch (e) {
      showErr(e.message);
      markField("prompt");
    }
  };
  $4("prompt").addEventListener("keydown", (ev) => {
    if ((ev.metaKey || ev.ctrlKey) && ev.key === "Enter") {
      ev.preventDefault();
      $4("runBtn").click();
    }
  });
  async function testRun(id) {
    try {
      const rt = await api2("POST", "/api/v1/routines/" + id + "/test-run");
      await refresh2();
      if (rt.run_id) {
        clearLog();
        stopPoll();
        activeRun = null;
        gotoPanel("run");
        startStream(rt.run_id);
      }
    } catch (e) {
      showErr(e.message);
    }
  }
  async function enableRtn(id) {
    try {
      await api2("POST", "/api/v1/routines/" + id + "/enable");
      toast3("routine enabled \u2014 it's live", "ok");
      await refresh2();
    } catch (e) {
      showErr(e.message);
    }
  }
  async function pauseRtn(id, paused) {
    try {
      await api2("POST", "/api/v1/routines/" + id + "/pause", { paused });
      toast3(paused ? "routine paused \u2014 it won't fire" : "routine resumed \u2014 it's live", "ok");
      await refresh2();
    } catch (e) {
      showErr(e.message);
    }
  }
  $4("probeBtn").onclick = async () => {
    try {
      const h = await api2("GET", "/api/v1/health/models");
      const tb = $4("health").querySelector("tbody");
      tb.innerHTML = (h.probes || []).map((p) => {
        const mm = p.qwen_mismatch ? " qwen_mismatch" : "";
        return `<tr><td>${esc3(p.id)}</td><td>${p.ok ? `<span class="chip ok">ok</span>` : `<span class="chip err">fail</span>`}</td><td>${esc3((p.ids || []).join(", "))}</td><td class="tag">${esc3(p.error || "")}${mm}</td></tr>`;
      }).join("") || `<tr><td colspan="4" class="tag">no probes returned</td></tr>`;
    } catch (e) {
      showErr(e.message);
    }
  };
  refresh2().catch((e) => showErr(e.message));
  loadMesh().catch((e) => showErr(e.message));
  loadHubs().catch((e) => showErr(e.message));
  renderMissions();
  renderSkillTemplates();
  renderRtnTriggers();
  boot();
  vibe();
  startStream(null);
  bootSupervision();
  var meshRefreshBtn = $4("meshRefresh");
  if (meshRefreshBtn) meshRefreshBtn.onclick = () => loadMesh().catch((e) => showErr(e.message));
  var hubsRefreshBtn = $4("hubsRefresh");
  if (hubsRefreshBtn) hubsRefreshBtn.onclick = () => loadHubs().catch((e) => showErr(e.message));
  var hubsDoctorBtn = $4("hubsDoctor");
  if (hubsDoctorBtn) hubsDoctorBtn.onclick = () => runDoctor().catch((e) => showErr(e.message));
  var hubDetailCloseBtn = $4("hubDetailClose");
  if (hubDetailCloseBtn) hubDetailCloseBtn.onclick = () => {
    const el2 = $4("hubDetail");
    if (el2) el2.hidden = true;
  };
  (function() {
    "use strict";
    if (window.__researchV2Loaded) return;
    window.__researchV2Loaded = true;
    const hud = document.createElement("div");
    hud.className = "hud";
    document.body.appendChild(hud);
    let hudTimer = null;
    window.hud = function(msg) {
      hud.textContent = msg;
      hud.classList.add("show");
      clearTimeout(hudTimer);
      hudTimer = setTimeout(() => hud.classList.remove("show"), 1600);
      return msg;
    };
    document.querySelectorAll('[data-num="1"]').forEach((el2) => {
      el2.classList.add("num");
    });
    document.querySelectorAll("[data-shortcut]").forEach((el2) => {
      if (el2.querySelector(".kbd")) return;
      const k = document.createElement("kbd");
      k.className = "kbd";
      k.textContent = el2.getAttribute("data-shortcut").trim();
      k.setAttribute("aria-hidden", "true");
      el2.appendChild(k);
    });
  })();
})();

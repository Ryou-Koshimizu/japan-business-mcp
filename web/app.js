/* 日本の業務 MCP — 遊び場
   ブラウザの中で Go の MCP サーバー（WebAssembly）を起動し、JSON-RPC をそのまま送って返事を見せる。 */
(() => {
  "use strict";

  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => [...el.querySelectorAll(s)];
  const html = document.documentElement;
  const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;
  const finePointer = matchMedia("(pointer: fine)").matches;
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

  // ── 文字の色（琥珀・緑）。この閲覧者の好みだけを覚える ──
  const toggle = $("#phosphor-toggle");
  const setPhosphor = (p) => {
    html.dataset.phosphor = p;
    toggle.setAttribute("aria-pressed", String(p === "green"));
    toggle.setAttribute("aria-label", p === "green" ? "文字の色を琥珀に切り替える" : "文字の色を緑に切り替える");
    $('meta[name="theme-color"]').setAttribute("content", p === "green" ? "#030a05" : "#0b0703");
  };
  try { const saved = localStorage.getItem("jbiz-phosphor"); if (saved === "green" || saved === "amber") setPhosphor(saved); } catch { /* 保存できない環境 */ }
  toggle.addEventListener("click", () => {
    const next = html.dataset.phosphor === "green" ? "amber" : "green";
    setPhosphor(next);
    try { localStorage.setItem("jbiz-phosphor", next); } catch { /* 保存できない環境 */ }
  });

  // ── 起動の画面 ──
  const bootLog = $("#boot-log");
  const bootFill = $("#boot-bar-fill");
  let bootDone = false;
  const bootLines = [];
  const bootPrint = async (line, speed = 6) => {
    if (bootDone || reduced) return;
    const row = document.createElement("span");
    bootLog.appendChild(row);
    bootLog.appendChild(document.createTextNode("\n"));
    bootLines.push(row);
    for (let i = 0; i <= line.length; i += 2) {
      if (bootDone) { row.textContent = line; return; }
      row.textContent = line.slice(0, i);
      await sleep(speed);
    }
    row.textContent = line;
  };
  const finishBoot = () => {
    if (bootDone) return;
    bootDone = true;
    html.classList.add("booted");
    startHero();
  };
  if (reduced) { html.classList.add("booted"); bootDone = true; }
  else {
    const skip = () => finishBoot();
    $("#boot").addEventListener("click", skip);
    addEventListener("keydown", skip, { once: true });
  }

  // ── WebAssembly の読み込み（gzip を自前で展開。進み具合は実際のバイト数） ──
  async function loadWasm() {
    const res = await fetch("main.wasm.gz");
    if (!res.ok) throw new Error(`main.wasm.gz を読めません（${res.status}）`);
    const total = Number(res.headers.get("content-length")) || 0;
    let got = 0;
    const counter = new TransformStream({
      transform(chunk, ctl) {
        got += chunk.byteLength;
        if (total) bootFill.style.width = `${Math.min(100, (got / total) * 100).toFixed(1)}%`;
        ctl.enqueue(chunk);
      },
    });
    const stream = res.body.pipeThrough(counter).pipeThrough(new DecompressionStream("gzip"));
    const bytes = await new Response(stream).arrayBuffer();
    bootFill.style.width = "100%";
    const go = new Go();
    const ready = new Promise((r) => { window.onJbizReady = r; });
    const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
    go.run(instance);
    await ready;
    return { compressed: got, raw: bytes.byteLength };
  }

  // ── MCP クライアント（JSON-RPC の文字列を送る） ──
  let nextId = 1;
  const listeners = new Set();
  async function rpc(method, params, { notify = false } = {}) {
    const msg = { jsonrpc: "2.0", method };
    if (params !== undefined) msg.params = params;
    if (!notify) msg.id = nextId++;
    const text = JSON.stringify(msg);
    const t0 = performance.now();
    const reply = await window.jbizMCP.send(text);
    const ms = performance.now() - t0;
    const parsed = reply ? JSON.parse(reply) : null;
    listeners.forEach((fn) => fn({ msg, reply: parsed, ms }));
    return { msg, reply: parsed, ms };
  }

  let tools = [];
  const handshake = [];
  let examples = [];
  let current = null;

  async function start() {
    const exPromise = fetch("examples.json").then((r) => r.json()).catch(() => []);
    bootPrint("JBIZ-MCP ROM v1 · 日本の業務 MCP サーバー");
    bootPrint("CPU ............ WebAssembly (GOOS=js GOARCH=wasm)");
    let info;
    try {
      const loading = loadWasm();
      await bootPrint("LOAD main.wasm.gz ...");
      info = await loading;
      await bootPrint(`  ${(info.compressed / 1048576).toFixed(1)} MB 受信 → ${(info.raw / 1048576).toFixed(1)} MB 展開 ... OK`);
    } catch (e) {
      await bootPrint(`ERROR ${e.message}`);
      setStatus(`起動を止めました：${e.message}`, true);
      finishBoot();
      return;
    }
    const init = await rpc("initialize", { protocolVersion: "2025-11-25", capabilities: {}, clientInfo: { name: "jbiz-playground", version: "1" } });
    await rpc("notifications/initialized", undefined, { notify: true });
    const si = init.reply.result.serverInfo;
    await bootPrint(`MCP initialize → ${si.name} ${si.version} (protocol ${init.reply.result.protocolVersion}) ... OK`);
    const tl = await rpc("tools/list", {});
    examples = await exPromise;
    // 並びは例（examples.json）の順＝仕事の流れの順に
    const order = [...new Set(examples.map((e) => e.tool))];
    tools = tl.reply.result.tools.slice().sort((a, b) => (order.indexOf(a.name) + 1 || 99) - (order.indexOf(b.name) + 1 || 99));
    handshake.push(["dir-out", `→ ${compact(init.msg, 120)}`], ["dir-in", `← ${si.name} ${si.version} · protocol ${init.reply.result.protocolVersion}`], ["dir-out", `→ ${compact(tl.msg)}`], ["dir-in", `← ${tools.length} tools: ${tools.map((t) => t.name).join(", ")}`]);
    await bootPrint(`tools/list → ${tools.length} tools ... OK`);
    const [rl, tpl, pl] = await Promise.all([rpc("resources/list", {}), rpc("resources/templates/list", {}), rpc("prompts/list", {})]);
    await bootPrint(`resources ${rl.reply.result.resources.length + tpl.reply.result.resourceTemplates.length} · prompts ${pl.reply.result.prompts.length} ... READY`);
    renderTools(tools);
    renderExtras(rl.reply.result.resources, tpl.reply.result.resourceTemplates, pl.reply.result.prompts);
    buildMenu();
    logEntry("out", init.msg, 0, true);
    logEntry("in", init.reply, init.ms, true);
    html.dataset.mcp = "ready";
    $("#run").disabled = false;
    setStatus(`接続：ブラウザ内の MCP サーバー（${si.version}）`);
    if (!bootDone) { await sleep(380); finishBoot(); }
    startMonitor();
  }

  function setStatus(text, err = false) {
    const st = $("#status");
    st.textContent = text;
    st.classList.toggle("err", err);
  }

  // ── ヒーロー：見出しを打つ ──
  let heroStarted = false;
  function startHero() {
    if (heroStarted) return;
    heroStarted = true;
    const title = $("#hero-title");
    if (reduced) { title.classList.add("done"); return; }
    const lines = $$(".type-line", title);
    lines.forEach((ln) => {
      const t = ln.textContent;
      ln.textContent = "";
      for (const c of t) {
        const s = document.createElement("span");
        s.className = "ch";
        s.textContent = c;
        ln.appendChild(s);
      }
    });
    (async () => {
      await sleep(320);
      for (const ln of lines) {
        ln.classList.add("typing");
        for (const ch of $$(".ch", ln)) { ch.classList.add("on"); await sleep(55 + Math.random() * 45); }
        ln.classList.remove("typing");
      }
      title.classList.add("done");
    })();
  }

  // ── ヒーローのモニター：例を順に本物のサーバーへ送って流す ──
  function compact(obj, max = 220) {
    const s = JSON.stringify(obj);
    return s.length > max ? `${s.slice(0, max)}…` : s;
  }
  // モニターの数字：tools/call の回数と平均の往復時間
  let calls = 0;
  let totalMs = 0;
  listeners.add(({ msg, ms }) => {
    if (msg.method !== "tools/call") return;
    calls++;
    totalMs += ms;
    const c = $("#ms-calls");
    const a = $("#ms-avg");
    c.textContent = String(calls);
    a.textContent = (totalMs / calls).toFixed(1);
    [c, a].forEach((el) => { el.classList.remove("bump"); void el.offsetWidth; el.classList.add("bump"); });
  });
  async function startMonitor() {
    $("#ms-tools").textContent = String(tools.length);
    const feed = $("#monitor-feed");
    const rtt = $("#monitor-rtt");
    if (!feed || getComputedStyle($(".monitor")).display === "none") return;
    let visible = true;
    new IntersectionObserver((es) => { visible = es[0].isIntersecting; }).observe(feed);
    const push = async (cls, text) => {
      const line = document.createElement("span");
      line.className = cls;
      feed.appendChild(line);
      feed.appendChild(document.createTextNode("\n"));
      if (reduced) line.textContent = text;
      else for (let i = 0; i <= text.length; i += 4) { line.textContent = text.slice(0, i); await sleep(8); }
      line.textContent = text;
      while (feed.childNodes.length > 40) feed.removeChild(feed.firstChild);
    };
    for (const h of handshake) await push(h[0], h[1]);
    let i = 0;
    for (;;) {
      if (!visible || document.hidden) { await sleep(600); continue; }
      const ex = examples[i++ % examples.length];
      const r = await rpc("tools/call", { name: ex.tool, arguments: ex.args });
      rtt.textContent = `${r.ms.toFixed(1)} ms`;
      await push("dir-out", `→ ${compact(r.msg)}`);
      const sc = r.reply.result && r.reply.result.structuredContent;
      const brief = sc ? (sc.summary || sc.output || compact(sc, 140)) : compact(r.reply);
      await push("dir-in", `← id ${r.reply.id} · ${brief}`);
      await sleep(reduced ? 4000 : 2600);
    }
  }

  // 道具の名前は「_」のあとでだけ折り返す
  function breakable(name) {
    const f = document.createDocumentFragment();
    name.split("_").forEach((part, i, all) => {
      f.append(part + (i < all.length - 1 ? "_" : ""));
      if (i < all.length - 1) f.append(document.createElement("wbr"));
    });
    return f;
  }

  // ── 遊び場 ──
  function buildMenu() {
    const menu = $("#tool-menu");
    menu.textContent = "";
    tools.forEach((t, idx) => {
      const b = document.createElement("button");
      b.type = "button";
      b.className = "tool-btn";
      b.setAttribute("role", "radio");
      b.setAttribute("aria-checked", "false");
      b.dataset.tool = t.name;
      b.tabIndex = idx === 0 ? 0 : -1;
      b.innerHTML = `<span class="tool-no">${String(idx + 1).padStart(2, "0")}</span><span class="tool-name"></span><span class="tool-title"></span>`;
      $(".tool-name", b).append(breakable(t.name));
      $(".tool-title", b).textContent = t.title || "";
      b.addEventListener("click", () => selectTool(t.name, true));
      b.addEventListener("keydown", (e) => {
        const keys = { ArrowDown: 1, ArrowRight: 1, ArrowUp: -1, ArrowLeft: -1 };
        if (!(e.key in keys)) return;
        e.preventDefault();
        const btns = $$(".tool-btn", menu);
        const n = btns[(btns.indexOf(b) + keys[e.key] + btns.length) % btns.length];
        n.focus();
        selectTool(n.dataset.tool, true);
      });
      menu.appendChild(b);
    });
    selectTool(tools[0].name, false);
  }

  function selectTool(name, focusKeep) {
    current = tools.find((t) => t.name === name);
    $$(".tool-btn").forEach((b) => {
      const on = b.dataset.tool === name;
      b.setAttribute("aria-checked", String(on));
      b.tabIndex = on ? 0 : -1;
    });
    $("#console-title").textContent = `${current.name}`;
    $("#tool-desc").textContent = `${current.title}：${current.description}`;
    const box = $("#examples");
    box.textContent = "";
    const mine = examples.filter((e) => e.tool === name);
    mine.forEach((ex, i) => {
      const c = document.createElement("button");
      c.type = "button";
      c.className = "chip";
      c.textContent = ex.label;
      c.dataset.example = ex.name;
      c.setAttribute("aria-pressed", String(i === 0));
      c.addEventListener("click", () => {
        $$(".chip", box).forEach((x) => x.setAttribute("aria-pressed", String(x === c)));
        setArgs(ex.args);
      });
      box.appendChild(c);
    });
    setArgs(mine.length ? mine[0].args : {});
    if (!focusKeep) return;
  }

  function setArgs(obj) {
    const ta = $("#args");
    ta.value = JSON.stringify(obj, null, 2);
    ta.classList.remove("invalid");
    fitArgs();
  }
  function fitArgs() {
    const ta = $("#args");
    ta.style.height = "auto";
    ta.style.height = `${ta.scrollHeight + 2}px`;
  }
  $("#args").addEventListener("input", fitArgs);

  function highlight(json) {
    const esc = json.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
    return esc.replace(/("(?:\\.|[^"\\])*")(\s*:)?|\b(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?|true|false|null)\b/g, (m, str, colon, num) => {
      if (str) return colon ? `<span class="tok-k">${str}</span>${colon}` : `<span class="tok-s">${str}</span>`;
      return `<span class="tok-n">${num}</span>`;
    });
  }

  // 1件の送受信をログに出す。文字は打つように流し、終わったら data-done を付ける。
  function logEntry(dir, obj, ms, instant = false) {
    const log = $("#log");
    const e = document.createElement("div");
    e.className = `entry ${dir}`;
    const isErr = dir === "in" && obj && ((obj.result && obj.result.isError) || obj.error);
    if (isErr) e.classList.add("err");
    const head = document.createElement("div");
    head.className = "entry-head";
    const method = dir === "out" ? obj.method : obj.result && obj.result.isError ? "isError: true" : obj.error ? "error" : "result";
    head.innerHTML = `<span class="arrow">${dir === "out" ? "→ 送信" : "← 受信"}</span><span></span><span></span>`;
    head.children[1].textContent = `${method}${obj && obj.id !== undefined ? ` · id ${obj.id}` : ""}`;
    head.children[2].textContent = dir === "in" ? `${ms.toFixed(1)} ms` : "";
    const pre = document.createElement("pre");
    const text = JSON.stringify(obj, null, 2);
    e.append(head, pre);
    log.appendChild(e);
    const done = () => { pre.innerHTML = highlight(text); e.dataset.done = "1"; log.scrollTop = log.scrollHeight; };
    if (instant || reduced) { done(); return Promise.resolve(); }
    return new Promise((resolve) => {
      const per = Math.max(24, Math.ceil(text.length / 36));
      let i = 0;
      const step = () => {
        i += per;
        if (i >= text.length) { done(); resolve(); return; }
        pre.textContent = text.slice(0, i);
        log.scrollTop = log.scrollHeight;
        requestAnimationFrame(step);
      };
      requestAnimationFrame(step);
    });
  }

  async function run() {
    const ta = $("#args");
    let args;
    try {
      args = JSON.parse(ta.value || "{}");
      if (typeof args !== "object" || Array.isArray(args) || args === null) throw new Error("引数は { } のオブジェクトで書きます");
    } catch (e) {
      ta.classList.add("invalid");
      showSummary(`引数の JSON を読めません：${e.message}`, true);
      return;
    }
    ta.classList.remove("invalid");
    const btn = $("#run");
    btn.disabled = true;
    const r = await rpc("tools/call", { name: current.name, arguments: args });
    await logEntry("out", r.msg, 0);
    await logEntry("in", r.reply, r.ms);
    const res = r.reply.result;
    if (r.reply.error) showSummary(r.reply.error.message, true);
    else if (res.isError) showSummary(res.content.map((c) => c.text).join(" "), true);
    else showSummary(summarize(res.structuredContent));
    setStatus(`往復 ${r.ms.toFixed(1)} ms · ブラウザ内の MCP サーバー`);
    btn.disabled = false;
    $("#play").dataset.lastId = String(r.msg.id);
  }

  function summarize(sc) {
    if (!sc) return "";
    if (sc.summary) return sc.summary;
    if (sc.output !== undefined) return sc.output;
    return "";
  }
  function showSummary(text, err = false) {
    const s = $("#summary");
    s.hidden = !text;
    s.textContent = text;
    s.classList.toggle("err", err);
    s.style.animation = "none";
    void s.offsetWidth;
    s.style.animation = "";
  }

  $("#run").addEventListener("click", run);
  $("#args").addEventListener("keydown", (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") { e.preventDefault(); if (!$("#run").disabled) run(); }
  });
  $("#clear").addEventListener("click", () => { $("#log").textContent = ""; showSummary(""); });

  // ── 道具の一覧（tools/list の中身） ──
  function renderTools(list) {
    const ls = $("#ls");
    ls.textContent = "";
    list.forEach((t) => {
      const row = document.createElement("div");
      row.className = "ls-row";
      row.setAttribute("role", "listitem");
      row.innerHTML = `<span class="ls-perm">-r-x</span><span class="ls-name"><button type="button" class="ls-try"></button></span><span class="ls-title"></span><span class="ls-desc"></span>`;
      const b = $(".ls-try", row);
      b.append(breakable(t.name));
      b.setAttribute("aria-label", `${t.name}（${t.title}）を遊び場で試す`);
      b.addEventListener("click", () => { selectTool(t.name, true); $("#play").scrollIntoView({ behavior: reduced ? "auto" : "smooth" }); });
      $(".ls-title", row).textContent = t.title;
      $(".ls-desc", row).textContent = t.description;
      ls.appendChild(row);
    });
  }
  function renderExtras(resources, templates, prompts) {
    const r = $("#resources");
    r.textContent = "";
    [...templates.map((t) => ({ uri: t.uriTemplate, title: t.title })), ...resources.map((x) => ({ uri: x.uri, title: x.title }))].forEach((x) => {
      const li = document.createElement("li");
      li.innerHTML = "<code></code> — <span></span>";
      li.children[0].textContent = x.uri;
      li.children[1].textContent = x.title;
      r.appendChild(li);
    });
    const p = $("#prompts");
    p.textContent = "";
    prompts.forEach((x) => {
      const li = document.createElement("li");
      li.innerHTML = "<code></code> — <span></span>";
      li.children[0].textContent = x.name;
      li.children[1].textContent = x.description;
      p.appendChild(li);
    });
  }

  // ── 導入のタブ ──
  const tabs = $$('[role="tab"]');
  const activate = (tab, focus) => {
    tabs.forEach((t) => {
      const on = t === tab;
      t.setAttribute("aria-selected", String(on));
      t.tabIndex = on ? 0 : -1;
      $(`#${t.getAttribute("aria-controls")}`).hidden = !on;
    });
    if (focus) tab.focus();
  };
  tabs.forEach((t, i) => {
    t.addEventListener("click", () => activate(t, false));
    t.addEventListener("keydown", (e) => {
      const d = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
      if (d) { e.preventDefault(); activate(tabs[(i + d + tabs.length) % tabs.length], true); }
    });
  });

  // ── 写すボタン ──
  $$(".copy").forEach((b) => b.addEventListener("click", async () => {
    const text = $(`#${b.dataset.copy}`).textContent;
    try { await navigator.clipboard.writeText(text); }
    catch {
      const ta = document.createElement("textarea");
      ta.value = text; document.body.appendChild(ta); ta.select();
      try { document.execCommand("copy"); } catch { /* 写せない環境 */ }
      ta.remove();
    }
    b.classList.add("done");
    $("use", b).setAttribute("href", "#i-check");
    setTimeout(() => { b.classList.remove("done"); $("use", b).setAttribute("href", "#i-copy"); }, 1400);
  }));

  // ── 確かめたこと（数字は stats.js：試験の記録から書き出したもの） ──
  function renderStats() {
    const box = $("#stats");
    (window.JBIZ_STATS || []).forEach((s, i) => {
      const d = document.createElement("div");
      d.className = `stat${i === 0 ? " big" : ""}`;
      d.innerHTML = `<div class="stat-num"><span class="n">0</span><span class="stat-unit"></span></div><div class="stat-label"></div><div class="stat-cond"></div>`;
      const n = $(".n", d);
      n.dataset.to = String(s.value);
      n.textContent = reduced ? s.value.toLocaleString("ja-JP") : "0";
      $(".stat-unit", d).textContent = s.unit;
      $(".stat-label", d).textContent = s.label;
      $(".stat-cond", d).textContent = s.cond;
      box.appendChild(d);
    });
  }
  function countUp(el) {
    const to = Number(el.dataset.to);
    if (reduced) { el.textContent = to.toLocaleString("ja-JP"); return; }
    const t0 = performance.now();
    const dur = 1400;
    const tick = (t) => {
      const p = Math.min(1, (t - t0) / dur);
      const e = 1 - Math.pow(1 - p, 4);
      el.textContent = Math.round(to * e).toLocaleString("ja-JP");
      if (p < 1) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }
  renderStats();

  // ── 出てくる動き ──
  const io = new IntersectionObserver((es) => {
    es.forEach((e) => {
      if (!e.isIntersecting) return;
      e.target.classList.add("in");
      $$(".n", e.target).forEach(countUp);
      io.unobserve(e.target);
    });
  }, { rootMargin: "0px 0px -8% 0px" });
  $$(".reveal").forEach((el) => (reduced ? el.classList.add("in") : io.observe(el)));

  // ── 磁石のボタン ──
  if (finePointer && !reduced) {
    $$(".magnetic").forEach((b) => {
      b.addEventListener("pointermove", (e) => {
        const r = b.getBoundingClientRect();
        b.style.setProperty("--mx", `${((e.clientX - r.left) / r.width - 0.5) * 10}px`);
        b.style.setProperty("--my", `${((e.clientY - r.top) / r.height - 0.5) * 8}px`);
      });
      b.addEventListener("pointerleave", () => { b.style.setProperty("--mx", "0px"); b.style.setProperty("--my", "0px"); });
    });
  }

  if (reduced) startHero();
  start().catch((e) => { setStatus(`起動を止めました：${e.message}`, true); finishBoot(); });
})();

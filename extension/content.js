// ODM Integration: shows a "Download this video" button over the page's
// video when the background script has seen a downloadable media response
// in this frame. Runs in every frame, because players are usually iframes.
(() => {
  if (window.__odmContent) return;
  window.__odmContent = true;

  const MIN_W = 200;
  const MIN_H = 120;
  let items = [];
  let dismissed = false;
  let host = null;
  let ui = null;
  // medialist.js runs first in the same content-script world.
  const { fmtSize, orderMedia, mediaRow, ROW_CSS, ICONS } = self.odmMediaList;

  // The largest visible video is the player; small ones are previews.
  function mainVideo() {
    let best = null;
    let bestArea = 0;
    for (const v of document.querySelectorAll("video")) {
      const r = v.getBoundingClientRect();
      if (r.width < MIN_W || r.height < MIN_H) continue;
      const area = r.width * r.height;
      if (area > bestArea) {
        best = v;
        bestArea = area;
      }
    }
    return best;
  }

  // The button lives in a closed shadow root so the page's CSS cannot
  // restyle or hide it, and ours cannot leak into the page. It is always
  // dark: it sits on top of video, whatever the page's own theme.
  function build() {
    host = document.createElement("div");
    host.style.cssText = "position:absolute;z-index:2147483647;top:0;left:0;";
    const root = host.attachShadow({ mode: "closed" });
    root.innerHTML = `
      <style>
        :host { all: initial; }
        .wrap { --odm-fg: #e6edf3; --odm-muted: #8b949e; --odm-hover: rgba(56, 139, 253, .18);
          --odm-icon-bg: #21262d; --odm-accent: #58a6ff;
          display: flex; flex-direction: column; align-items: flex-end; gap: 6px;
          font: 13px/1.35 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; color: var(--odm-fg); }
        .bar { display: flex; gap: 4px; }
        .bar button { all: unset; box-sizing: border-box; cursor: pointer; height: 32px; border-radius: 8px;
          display: inline-flex; align-items: center; box-shadow: 0 2px 10px rgba(0, 0, 0, .45); }
        .bar button:focus-visible { outline: 2px solid #58a6ff; outline-offset: 2px; }
        .bar .dl { gap: 7px; padding: 0 13px 0 11px; background: #1f6feb; color: #fff; font-weight: 600; white-space: nowrap; }
        .bar .dl:hover { background: #388bfd; }
        .bar .x { width: 32px; justify-content: center; background: rgba(22, 27, 34, .92); color: #c9d1d9; }
        .bar .x:hover { background: #30363d; color: #fff; }
        .bar svg { width: 16px; height: 16px; }
        .menu { box-sizing: border-box; width: 320px; max-width: calc(100vw - 16px); max-height: 340px; overflow-y: auto; padding: 4px;
          background: rgba(22, 27, 34, .97); border: 1px solid #30363d; border-radius: 12px;
          box-shadow: 0 10px 30px rgba(0, 0, 0, .55); }
        .menu .head { padding: 6px 10px 4px; color: var(--odm-muted); font-size: 11px; font-weight: 600;
          letter-spacing: .05em; text-transform: uppercase; }
        .status { padding: 7px 12px; border-radius: 8px; background: rgba(22, 27, 34, .97);
          border: 1px solid #30363d; box-shadow: 0 6px 20px rgba(0, 0, 0, .5); }
        .status.ok { border-color: rgba(63, 185, 80, .6); }
        .status.err { border-color: rgba(248, 81, 73, .7); }
        [hidden] { display: none !important; }
        ${ROW_CSS}
      </style>
      <div class="wrap">
        <div class="bar">
          <button class="dl" type="button"><span class="ico">${ICONS.download}</span><span class="txt"></span></button>
          <button class="x" type="button" title="Hide" aria-label="Hide">${ICONS.close}</button>
        </div>
        <div class="menu" role="list" aria-label="Media found on this page" hidden></div>
        <div class="status" role="status" aria-live="polite" hidden></div>
      </div>`;
    ui = {
      dl: root.querySelector(".dl"),
      dlText: root.querySelector(".dl .txt"),
      x: root.querySelector(".x"),
      menu: root.querySelector(".menu"),
      status: root.querySelector(".status"),
    };
    ui.x.addEventListener("click", (e) => {
      e.stopPropagation();
      dismissed = true;
      host.remove();
    });
    ui.dl.addEventListener("click", (e) => {
      e.stopPropagation();
      if (items.length === 0) sendPage();
      else if (items.length === 1) send(items[0]);
      else toggleMenu();
    });
    // A click anywhere else closes the menu. Capture phase: player overlays
    // often stop clicks from bubbling.
    document.addEventListener("click", (e) => {
      if (ui && !ui.menu.hidden && !e.composedPath().includes(host)) closeMenu(false);
    }, true);
    root.addEventListener("keydown", (e) => {
      if (e.key === "Escape" && !ui.menu.hidden) {
        e.stopPropagation();
        closeMenu(true);
      }
    });
    document.documentElement.appendChild(host);
  }

  // menuKey is what the open menu shows; it is redrawn only when that
  // changes, not on every reload of a live playlist (which would reset its
  // scroll and focus, and could swallow a click).
  let menuKey = "";
  const keyOf = (list) => JSON.stringify(list.map((i) => [i.url, i.quality, i.live, i.master, i.size]));

  function renderMenu() {
    const key = keyOf(items);
    if (key === menuKey && ui.menu.childElementCount) return;
    menuKey = key;
    const scroll = ui.menu.scrollTop;
    const focused = [...ui.menu.querySelectorAll(".odm-row")].indexOf(ui.menu.getRootNode().activeElement);
    ui.menu.replaceChildren();
    const head = document.createElement("div");
    head.className = "head";
    head.textContent = items.length === 1 ? "1 item found" : `${items.length} items found`;
    ui.menu.appendChild(head);
    for (const it of orderMedia(items)) {
      const row = mediaRow(document, it);
      row.addEventListener("click", (e) => {
        e.stopPropagation();
        closeMenu(true);
        send(it);
      });
      row.setAttribute("role", "listitem");
      ui.menu.appendChild(row);
    }
    ui.menu.scrollTop = scroll;
    const rows = ui.menu.querySelectorAll(".odm-row");
    if (focused >= 0 && rows[Math.min(focused, rows.length - 1)]) rows[Math.min(focused, rows.length - 1)].focus();
  }

  function closeMenu(refocus) {
    ui.menu.hidden = true;
    if (refocus) ui.dl.focus();
    schedule();
  }

  function toggleMenu() {
    if (!ui.menu.hidden) {
      closeMenu(false);
      return;
    }
    menuKey = "";
    renderMenu();
    ui.menu.hidden = false;
    const first = ui.menu.querySelector(".odm-row");
    if (first) first.focus({ preventScroll: true });
    schedule(); // the menu widens the box: place it again at once
  }

  let flashTimer = 0;
  function flash(text, ok) {
    ui.status.textContent = text;
    ui.status.className = "status " + (ok ? "ok" : "err");
    ui.status.hidden = false;
    clearTimeout(flashTimer);
    flashTimer = setTimeout(() => (ui.status.hidden = true), 3000);
  }

  function send(item) {
    chrome.runtime.sendMessage({ type: "odm-grab", item }, (reply) => {
      if (chrome.runtime.lastError || !reply) flash("ODM is not reachable", false);
      else if (!reply.ok) flash(`ODM: ${reply.error}`, false);
      else flash(reply.message === "already in the list" ? "Already in ODM" : "Sent to ODM", true);
    });
  }

  // sendPage hands off the page itself, not a sniffed media URL: the
  // fallback for a visible <video> whose stream this content script never
  // saw a recognizable network response for (an encrypted/obfuscated
  // segment format, a site that renders into a blob: URL, …). ODM's own
  // New Download dialog tries yt-dlp on it — the same fallback a typed
  // address gets there, covering far more sites than this script's own
  // sniffing ever could.
  function sendPage() {
    chrome.runtime.sendMessage({ type: "odm-grab-page", url: location.href, title: document.title }, (reply) => {
      if (chrome.runtime.lastError || !reply) flash("ODM is not reachable", false);
      else if (!reply.ok) flash(`ODM: ${reply.error}`, false);
      else flash("Sent to ODM", true);
    });
  }

  function label() {
    if (items.length === 0) return "Download this video";
    const files = items.filter((i) => i.kind === "file");
    const best = files.sort((a, b) => b.size - a.size)[0];
    if (!best) return "Download this video";
    return `Download this video${best.size ? " · " + fmtSize(best.size) : ""}`;
  }

  // Keeps the button at the video's top-right corner as the page scrolls,
  // resizes or swaps players.
  let raf = 0;
  function place() {
    raf = 0;
    if (dismissed) return;
    const v = mainVideo();
    // Nothing sniffed and no visible <video> either: nothing to offer.
    // A visible <video> with no sniffed media is exactly the fallback
    // case sendPage() exists for (see its own comment).
    if (items.length === 0 && !v) return;
    if (!v) {
      if (host) host.style.display = "none";
      return;
    }
    if (!host) build();
    host.style.display = "";
    ui.dlText.textContent = label();
    const r = v.getBoundingClientRect();
    const w = host.getBoundingClientRect().width || 220;
    host.style.top = `${window.scrollY + r.top + 8}px`;
    host.style.left = `${window.scrollX + Math.max(r.left, r.right - w - 8)}px`;
  }
  const schedule = () => {
    if (!raf) raf = requestAnimationFrame(place);
  };

  function setItems(list) {
    items = Array.isArray(list) ? list : [];
    // An open menu follows the list (a label filled in by a peek, a new
    // stream), keeping its place.
    if (ui && !ui.menu.hidden) renderMenu();
    schedule();
  }

  chrome.runtime.onMessage.addListener((msg) => {
    if (msg && msg.type === "odm-media") setItems(msg.items);
  });
  chrome.runtime.sendMessage({ type: "odm-media-request" }, (list) => {
    if (!chrome.runtime.lastError) setItems(list);
  });

  window.addEventListener("scroll", schedule, { passive: true });
  window.addEventListener("resize", schedule, { passive: true });
  setInterval(schedule, 1000); // players resize or replace the <video> element

  // Alt-click on a link: let the browser download it this once.
  document.addEventListener("click", (e) => {
    if (!e.altKey) return;
    const a = e.target instanceof Element && e.target.closest("a[href]");
    if (a) chrome.runtime.sendMessage({ type: "odm-bypass", url: a.href });
  }, true);
})();

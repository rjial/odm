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
  // singleURL, when set, overrides items/menu entirely: ui.dl hands this
  // one URL off directly instead. Set by placeSingle, cleared by
  // placeMain — see place()'s own comment for why.
  let singleURL = null;
  // medialist.js runs first in the same content-script world.
  const { fmtSize, orderMedia, mediaRow, ROW_CSS, ICONS } = self.odmMediaList;

  // qualifyingVideos lists every on-screen video big enough to be a
  // player rather than a preview thumbnail. A vertical reels-style feed
  // (Instagram's, TikTok's) keeps the next/previous item's <video>
  // mounted just off-screen for a smooth scroll, with real dimensions
  // too, so an actual viewport check (not just width/height) is what
  // keeps those from counting.
  function qualifyingVideos() {
    const list = [];
    for (const v of document.querySelectorAll("video")) {
      const r = v.getBoundingClientRect();
      if (r.width < MIN_W || r.height < MIN_H) continue;
      if (r.bottom <= 0 || r.top >= window.innerHeight || r.right <= 0 || r.left >= window.innerWidth) continue;
      list.push(v);
    }
    return list;
  }

  // currentPermalink reports the current URL when it already names one
  // specific reel/post, not a feed or grid of several — Instagram pushes
  // a per-item address via the History API as a reels feed is scrolled,
  // even though (per qualifyingVideos' own comment) the DOM still holds
  // the next/previous item's elements too. Far more reliable than
  // hunting the DOM for a link: a reel's own info panel links to things
  // like its audio track (.../reels/audio/<id>/), which can otherwise be
  // mistaken for a second reel's permalink (nearestPostURL's own anchored
  // pattern guards the same way, for the sites it does have to search).
  function currentPermalink() {
    return /^\/(p|reel|reels|tv)\/[^/]+\/?$/.test(location.pathname) ? location.href : null;
  }

  // The largest visible video is the player; small ones are previews.
  // Used only once sniffed media narrows things to one player per page
  // (true of every site this was built against); a feed or grid page with
  // several videos at once and nothing sniffed uses qualifyingVideos
  // instead, one button each — see placeFallbacks.
  function mainVideo() {
    let best = null;
    let bestArea = 0;
    for (const v of qualifyingVideos()) {
      const r = v.getBoundingClientRect();
      const area = r.width * r.height;
      if (area > bestArea) {
        best = v;
        bestArea = area;
      }
    }
    return best;
  }

  // nearestPostURL finds a video's own permalink by walking up to the
  // closest wrapping link, for a feed/grid page with several videos at
  // once and no specific one already named by the URL (a profile's own
  // grid of posts, say — currentPermalink is checked first and covers
  // the reels-feed case): the page's own URL is the same for every video
  // on it there, so handing that off no matter which button was clicked
  // would always resolve the same one post. "/p/", "/reel/" and "/tv/"
  // are Instagram's own permalink shapes; the pattern is anchored to
  // exactly two path segments so a post's own links to other things
  // (its audio track, at .../reels/audio/<id>/) are not mistaken for a
  // second post's permalink. Falling back to the page URL (handled by
  // the caller) is no worse than today's single-video behavior for a
  // site this heuristic misses.
  function nearestPostURL(video) {
    let el = video;
    while (el && el !== document.body) {
      if (el.tagName === "A" && el.getAttribute("href")) {
        try {
          const u = new URL(el.href, location.href);
          if (/^\/(p|reel|reels|tv)\/[^/]+\/?$/.test(u.pathname)) return u.href;
        } catch { /* not a parseable href */ }
      }
      el = el.parentElement;
    }
    return null;
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
      if (singleURL) {
        const url = singleURL;
        chrome.runtime.sendMessage({ type: "odm-grab-page", url, title: document.title }, (reply) => {
          if (chrome.runtime.lastError || !reply) flash("ODM is not reachable", false);
          else if (!reply.ok) flash(`ODM: ${reply.error}`, false);
          else flash("Sent to ODM", true);
        });
        return;
      }
      if (items.length === 1) send(items[0]);
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

  function label() {
    const files = items.filter((i) => i.kind === "file");
    const best = files.sort((a, b) => b.size - a.size)[0];
    if (!best) return "Download this video";
    return `Download this video${best.size ? " · " + fmtSize(best.size) : ""}`;
  }

  // Keeps the button at the video's top-right corner as the page scrolls,
  // resizes or swaps players.
  function placeMain() {
    singleURL = null;
    const v = mainVideo();
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

  // placeSingle offers one confident button for a page whose own URL
  // already names one specific reel/post (currentPermalink), ignoring
  // items/the menu entirely: a reels-style feed preloads several items'
  // media at once, often faster than any per-navigation reset of the
  // sniffed list can clear it (see currentPermalink's own comment), so
  // that list can hold more than just the one actually in view with no
  // reliable way to tell which is which — the URL alone is trustworthy.
  function placeSingle(url) {
    singleURL = url;
    const v = mainVideo();
    if (!v) {
      if (host) host.style.display = "none";
      return;
    }
    if (!host) build();
    host.style.display = "";
    ui.dlText.textContent = "Download this video";
    const r = v.getBoundingClientRect();
    const w = host.getBoundingClientRect().width || 220;
    host.style.top = `${window.scrollY + r.top + 8}px`;
    host.style.left = `${window.scrollX + Math.max(r.left, r.right - w - 8)}px`;
  }

  // fallbacks holds one tiny button per qualifying video for the "nothing
  // sniffed" case: a feed or grid page (Instagram's, for instance) can
  // have several of those at once, each needing its own, unlike the one
  // main player placeMain positions a single button over.
  const fallbacks = new Map(); // video element -> { host, txt }

  function buildFallback(video) {
    const fhost = document.createElement("div");
    fhost.style.cssText = "position:absolute;z-index:2147483647;top:0;left:0;";
    const root = fhost.attachShadow({ mode: "closed" });
    root.innerHTML = `
      <style>
        :host { all: initial; }
        button { all: unset; box-sizing: border-box; cursor: pointer; height: 32px; border-radius: 8px;
          display: inline-flex; align-items: center; gap: 7px; padding: 0 13px 0 11px;
          background: #1f6feb; color: #fff; font-weight: 600; white-space: nowrap;
          font: 13px/1.35 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
          box-shadow: 0 2px 10px rgba(0, 0, 0, .45); }
        button:hover { background: #388bfd; }
        button:focus-visible { outline: 2px solid #58a6ff; outline-offset: 2px; }
        svg { width: 16px; height: 16px; }
      </style>
      <button type="button"><span class="ico">${ICONS.download}</span><span class="txt">Download this video</span></button>`;
    const button = root.querySelector("button");
    const txt = root.querySelector(".txt");
    let flashTimer = 0;
    button.addEventListener("click", (e) => {
      e.stopPropagation();
      const url = currentPermalink() || nearestPostURL(video) || location.href;
      chrome.runtime.sendMessage({ type: "odm-grab-page", url, title: document.title }, (reply) => {
        const ok = !chrome.runtime.lastError && reply && reply.ok;
        txt.textContent = ok ? "Sent to ODM" : "ODM is not reachable";
        clearTimeout(flashTimer);
        flashTimer = setTimeout(() => (txt.textContent = "Download this video"), 2500);
      });
    });
    document.documentElement.appendChild(fhost);
    return { host: fhost, txt };
  }

  function placeFallbacks() {
    const current = new Set(qualifyingVideos());
    for (const v of current) {
      let entry = fallbacks.get(v);
      if (!entry) {
        entry = buildFallback(v);
        fallbacks.set(v, entry);
      }
      const r = v.getBoundingClientRect();
      const w = entry.host.getBoundingClientRect().width || 220;
      entry.host.style.display = "";
      entry.host.style.top = `${window.scrollY + r.top + 8}px`;
      entry.host.style.left = `${window.scrollX + Math.max(r.left, r.right - w - 8)}px`;
    }
    for (const [v, entry] of fallbacks) {
      if (!current.has(v)) {
        entry.host.remove();
        fallbacks.delete(v);
      }
    }
  }

  function removeFallbacks() {
    for (const [, entry] of fallbacks) entry.host.remove();
    fallbacks.clear();
  }

  let raf = 0;
  function place() {
    raf = 0;
    if (dismissed) {
      removeFallbacks();
      return;
    }
    // A specific reel/post's own URL beats any sniffed media list — see
    // placeSingle's own comment for why.
    const permalink = currentPermalink();
    if (permalink) {
      removeFallbacks();
      placeSingle(permalink);
      return;
    }
    if (items.length > 0) {
      removeFallbacks();
      placeMain();
      return;
    }
    if (host) host.style.display = "none";
    placeFallbacks();
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

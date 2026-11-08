// The download button on a video's player, running inside the site; video.js
// has the rules and video-worker.js does the sending. It floats over the
// middle of the video's top edge, between the corners and the bottom bar where
// players keep their own controls, and shows while the pointer moves over the
// video, as those controls do.
//
// It lives in a closed shadow root of its own, so the site's styles and
// scripts do not reach it, and it reads nothing of the site but the address
// and where the largest video sits. That anchors it on the video element and
// on the element shown in fullscreen, never on the site's class names.

(() => {
  // A tab can get this twice: from its registration, and from
  // injectVideoButton for a page that was still loading as the button went on.
  if (globalThis.knightloaderVideoButton) return;
  globalThis.knightloaderVideoButton = true;

  // Smaller videos are previews and ads, not the player.
  const MIN_WIDTH = 200;
  const MIN_HEIGHT = 112;
  // About as long as the players keep their controls up after the pointer
  // stops.
  const IDLE_MS = 2500;
  const RESULT_MS = 4000;
  const STORED = ['videoButton', 'videoButtonHidden', 'theme', 'accent', 'shape'];
  // What sendVideo in video-worker.js answers, each with its label.
  const RESULTS = ['sent', 'already', 'held', 'refused', 'offline', 'unpaired', 'failed'];
  const SVG = 'http://www.w3.org/2000/svg';
  // GlimStone's IconDownload, Streamline interface-essential/download-circle.svg (CC BY 4.0).
  const DOWNLOAD =
    'M7 14c3.866 0 7 -3.134 7 -7 0 -3.86599 -3.134 -7 -7 -7 -3.86599 0 -7 3.13401 -7 7 0 3.866 3.13401 7 7 7Zm3.1464 -5.68958L7.3535 11.1033c-0.19526 0.1953 -0.51184 0.1953 -0.7071 0L3.8535 8.31042c-0.31498 -0.31499 -0.09189 -0.85356 0.35356 -0.85356h1.79289l0 -3.70686c0 -0.55228 0.44772 -1 1 -1 0.55229 0 1 0.44772 1 1l0 3.70686h1.79289c0.44546 0 0.66856 0.53857 0.35356 0.85356Z';
  const CARET = 'M1.4 1.4 L8.6 1.4 L5 4.6 Z';
  const RADIUS = { round: '9999px', soft: '5px', square: '0px', leaf: '18px' };
  // What a player draws over its video for people to use, whatever the site
  // calls it.
  const CONTROLS = 'a, button, input, select, textarea, [role="button"], [role="link"], [role="slider"], [role="checkbox"], [role="menuitem"]';

  // GlimStone's tokens and its subtle motion level, which the extension's own
  // pages run at, since glimstone.css does not reach into a shadow root.
  const STYLE = `
    :host { all: initial; position: fixed; z-index: 2147483647; translate: -50% 0; }
    .kl {
      --surface: #262626; --raised: #393939; --text: #f4f4f4; --sub: #c6c6c6;
      --ok: #6fdc8c; --fail: #ff8389; --line: rgba(255, 255, 255, .08);
      --elevation: 0 1px 2px rgba(0, 0, 0, .40), 0 18px 40px -24px rgba(0, 0, 0, .85);
      font: 500 14px/1.4 "Noto Sans Variable", "Noto Sans", system-ui, sans-serif;
      color: var(--text);
      opacity: 0;
      pointer-events: none;
      transition: opacity 140ms ease;
    }
    .kl[data-theme="light"] {
      --surface: #ffffff; --raised: #e8e8e8; --text: #161616; --sub: #525252;
      --ok: #0e6027; --fail: #da1e28; --line: rgba(0, 0, 0, .08);
      --elevation: 0 1px 2px rgba(0, 0, 0, .06), 0 16px 36px -26px rgba(0, 0, 0, .35);
    }
    .kl.shown { opacity: 1; pointer-events: auto; }
    .bar { display: flex; gap: 2px; border-radius: var(--radius); box-shadow: var(--elevation); }
    button {
      all: unset;
      box-sizing: border-box;
      display: inline-flex; align-items: center; justify-content: center; gap: 8px;
      height: 32px;
      cursor: pointer;
      white-space: nowrap;
      transition: background-color 80ms ease, color 80ms ease;
    }
    button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
    .send {
      padding: 0 14px;
      background: var(--accent); color: var(--accent-contrast);
      border-start-start-radius: var(--radius); border-end-start-radius: var(--radius);
    }
    .send:hover { background: color-mix(in srgb, var(--accent) 85%, var(--text)); }
    .more {
      width: 32px;
      background: var(--surface); color: var(--sub);
      border-start-end-radius: var(--radius); border-end-end-radius: var(--radius);
    }
    .more:hover, .more[aria-expanded="true"] { background: var(--raised); color: var(--text); }
    .kl[data-state="sending"] .send { animation: pulse 3.2s ease-in-out infinite; }
    .kl[data-state="sent"] .send, .kl[data-state="already"] .send { background: var(--surface); color: var(--ok); }
    .kl[data-state="held"] .send, .kl[data-state="refused"] .send, .kl[data-state="offline"] .send,
    .kl[data-state="unpaired"] .send, .kl[data-state="failed"] .send { background: var(--surface); color: var(--fail); }
    .menu {
      position: absolute; top: 40px; left: 50%; translate: -50% 0;
      min-width: 180px; padding: 4px;
      display: flex; flex-direction: column;
      background: var(--surface);
      border-radius: min(var(--radius), 10px);
      box-shadow: var(--elevation), inset 0 0 0 1px var(--line);
    }
    .menu[hidden] { display: none; }
    .menu button { justify-content: flex-start; height: 30px; padding: 0 10px; border-radius: min(var(--radius), 6px); color: var(--text); }
    .menu button:hover, .menu button:focus-visible { background: var(--raised); outline: none; }
    .menu [role="menuitemcheckbox"]::after { content: ""; margin-inline-start: auto; width: 14px; }
    .menu [role="menuitemcheckbox"][aria-checked="true"]::after { content: "\\2713"; color: var(--accent); }
    .menu [role="separator"] { height: 1px; margin: 4px 6px; background: var(--line); }
    .note { display: flex; align-items: center; min-height: 30px; padding: 0 10px; color: var(--sub); }
    .menu[aria-busy="true"] .note { animation: pulse 3.2s ease-in-out infinite; }
    @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: .45; } }
    @keyframes shake {
      0%, 100% { transform: translateX(0); } 20% { transform: translateX(-4px); } 40% { transform: translateX(4px); }
      60% { transform: translateX(-2px); } 80% { transform: translateX(2px); }
    }
    @keyframes shake-reduced { 0%, 100% { opacity: 1; } 50% { opacity: .5; } }
    @keyframes confirm {
      0% { box-shadow: 0 0 0 0 color-mix(in srgb, var(--ok) 30%, transparent); scale: 1; }
      40% { box-shadow: 0 0 0 3px color-mix(in srgb, var(--ok) 30%, transparent); scale: 1.025; }
      100% { box-shadow: 0 0 0 0 transparent; scale: 1; }
    }
    @media (prefers-reduced-motion: no-preference) {
      .send.shake { animation: shake 220ms ease-in-out; }
      .send.confirm { animation: confirm 260ms ease-out; }
    }
    @media (prefers-reduced-motion: reduce) {
      .kl, button { transition: none; }
      .kl[data-state="sending"] .send, .menu[aria-busy="true"] .note { animation: none; }
      .send.shake { animation: shake-reduced 220ms ease-in-out; }
    }
  `;

  let stored = {};
  let labels = null;
  let page = null;
  let video = null;
  let pointerAt = 0;
  let hovering = false;
  let following = 0;
  let drop = 12;
  let playlist = false;
  let sending = false;
  let result = null;
  let resultTimer = 0;
  // What the instance said the video has: the address asked about, and its
  // answer once there is one.
  let offered = null;

  const host = document.createElement('knightloader-video-button');
  const root = host.attachShadow({ mode: 'closed' });
  const style = document.createElement('style');
  style.textContent = STYLE;
  const frame = document.createElement('div');
  frame.className = 'kl';
  const bar = document.createElement('div');
  bar.className = 'bar';
  const send = document.createElement('button');
  send.type = 'button';
  send.className = 'send';
  const sendLabel = document.createElement('span');
  send.append(icon(DOWNLOAD, '0 0 14 14', 14), sendLabel);
  const more = document.createElement('button');
  more.type = 'button';
  more.className = 'more';
  more.setAttribute('aria-haspopup', 'menu');
  more.setAttribute('aria-expanded', 'false');
  more.append(icon(CARET, '0 0 10 6', 10));
  bar.append(send, more);
  const menu = document.createElement('div');
  menu.className = 'menu';
  menu.setAttribute('role', 'menu');
  menu.hidden = true;
  frame.append(bar, menu);
  root.append(style, frame);

  function icon(d, box, size) {
    const svg = document.createElementNS(SVG, 'svg');
    svg.setAttribute('viewBox', box);
    svg.setAttribute('width', String(size));
    svg.setAttribute('height', String(size));
    svg.setAttribute('aria-hidden', 'true');
    const path = document.createElementNS(SVG, 'path');
    path.setAttribute('d', d);
    path.setAttribute('fill', 'currentColor');
    path.setAttribute('fill-rule', 'evenodd');
    svg.append(path);
    return svg;
  }

  /**
   * The video with the most of itself on screen, which is the player's. Shorts
   * keep the next ones loaded below the one playing, and the small ones are
   * previews and ads.
   */
  function largestVideo() {
    let best = null;
    let most = 0;
    for (const v of document.querySelectorAll('video')) {
      const r = v.getBoundingClientRect();
      if (r.width < MIN_WIDTH || r.height < MIN_HEIGHT) continue;
      const seen = Math.max(0, Math.min(r.right, innerWidth) - Math.max(r.left, 0)) * Math.max(0, Math.min(r.bottom, innerHeight) - Math.max(r.top, 0));
      if (seen > most) {
        best = v;
        most = seen;
      }
    }
    return best;
  }

  /**
   * Where the button can sit: the page, or in fullscreen the element shown
   * there, since nothing outside it is drawn. A video that is itself in
   * fullscreen has the browser's own controls and no room for the button.
   */
  function placeFor(v) {
    const full = document.fullscreenElement;
    if (!full) return document.documentElement;
    return full !== v && full.contains(v) ? full : null;
  }

  /**
   * Whether a spot shows the video or its player's own layers, rather than
   * something of the site in front of it, such as a header the video has
   * been scrolled under. The player is told apart by its size: what the video
   * and the spot have in common is about as tall as the video, where a page
   * is far taller.
   */
  function onPlayer(x, y) {
    const hit = document.elementsFromPoint(x, y).find((el) => el !== host);
    if (!hit) return false;
    let common = video;
    while (common && !common.contains(hit)) common = common.parentElement;
    return !!common && common.getBoundingClientRect().height <= video.getBoundingClientRect().height * 1.5 + 1;
  }

  /**
   * How far below the video's top edge the button sits: the first place,
   * from 12px down, where it covers none of the player's controls. Embedded
   * players keep a title and buttons along the top, a watch page keeps none.
   */
  function dropFor(r) {
    const width = host.getBoundingClientRect().width || 180;
    const xs = [r.left + r.width / 2 - width / 2 + 4, r.left + r.width / 2, r.left + r.width / 2 + width / 2 - 4];
    const covers = (y) => xs.some((x) => document.elementsFromPoint(x, y).find((el) => el !== host)?.closest?.(CONTROLS));
    for (let d = 12; d < r.height * 0.4; d += 8) {
      if (!covers(r.top + d + 2) && !covers(r.top + d + 30)) return d;
    }
    return 12;
  }

  /** Brings the button in line with the page, the video and the switch. */
  function update() {
    const next = videoPage(location.href);
    if (next?.url !== page?.url || next?.playlist !== page?.playlist) {
      // Another video: whatever the last one came to is not this one's.
      playlist = false;
      clearTimeout(resultTimer);
      result = null;
      closeMenu();
    }
    page = next;
    video = page && labels && videoButtonWanted(stored, page.site) ? largestVideo() : null;
    const place = video && placeFor(video);
    if (!place) {
      closeMenu();
      host.remove();
      return;
    }
    if (host.parentNode !== place) place.append(host);
    render();
    drop = dropFor(video.getBoundingClientRect());
    position();
  }

  /** Follows the video while the button shows, a frame at a time. */
  function position() {
    if (!video || !host.isConnected) return;
    const r = video.getBoundingClientRect();
    const x = Math.round(r.left + r.width / 2);
    const y = Math.round(r.top + drop);
    host.style.setProperty('left', `${x}px`);
    host.style.setProperty('top', `${y}px`);
    const wanted = hovering || !menu.hidden || sending || result !== null || performance.now() - pointerAt < IDLE_MS;
    const shown = wanted && (hovering || onPlayer(x, y + 16));
    frame.classList.toggle('shown', shown);
    if (shown && !following) {
      following = requestAnimationFrame(() => {
        following = 0;
        position();
      });
    }
  }

  function render() {
    const look = lookOf(stored);
    frame.dataset.theme = look.theme;
    frame.style.setProperty('--accent', look.accent);
    frame.style.setProperty('--accent-contrast', look.ink);
    frame.style.setProperty('--radius', look.radius);
    frame.dir = labels.dir;
    frame.lang = labels.lang;
    frame.dataset.state = sending ? 'sending' : (result?.result ?? '');
    sendLabel.textContent = sending ? labels.sending : result ? labels[result.result] : labels.send;
    const tip = result?.reason || (result ? labels[result.result] : labels.sendTip);
    send.setAttribute('aria-label', tip);
    send.title = tip;
    send.setAttribute('aria-busy', String(sending));
    more.setAttribute('aria-label', labels.more);
    more.title = labels.more;
  }

  /** The look the extension's own pages have, from the same stored choices
   *  (appearance.js). */
  function lookOf(s) {
    const dark = s.theme === 'dark' || (s.theme !== 'light' && !matchMedia('(prefers-color-scheme: light)').matches);
    const chosen = s.accent === SYSTEM_ACCENT ? browserAccent() : s.accent;
    const accent = validHex(chosen) ? chosen : DEFAULT_ACCENT;
    return { theme: dark ? 'dark' : 'light', accent, ink: contrastOn(accent), radius: RADIUS[s.shape] ?? RADIUS[DEFAULT_SHAPE] };
  }

  async function sendVideo(format) {
    if (sending || !page) return;
    closeMenu();
    if (result?.result === 'unpaired') {
      chrome.runtime.sendMessage({ type: 'knightloader-video-setup' }).catch(() => {});
      return;
    }
    clearTimeout(resultTimer);
    result = null;
    sending = true;
    update();
    const answer = await chrome.runtime
      .sendMessage({ type: 'knightloader-video-send', href: location.href, format, playlist })
      .catch(() => null);
    sending = false;
    result = RESULTS.includes(answer?.result) ? answer : { result: 'failed' };
    update();
    replay(send, result.result === 'sent' || result.result === 'already' ? 'confirm' : 'shake');
    resultTimer = setTimeout(() => {
      result = null;
      update();
    }, RESULT_MS);
  }

  /** Plays a one-shot animation again, as replay in shared.js does. */
  function replay(el, cls) {
    el.classList.remove('confirm', 'shake');
    // Reading a layout property flushes the removal.
    void el.offsetWidth;
    el.classList.add(cls);
  }

  function menuItem(text, role, onPick) {
    const item = document.createElement('button');
    item.type = 'button';
    item.setAttribute('role', role);
    item.textContent = text;
    item.addEventListener('click', (e) => {
      if (e.isTrusted) onPick();
    });
    return item;
  }

  function separator() {
    const line = document.createElement('div');
    line.setAttribute('role', 'separator');
    return line;
  }

  function note(text) {
    const line = document.createElement('div');
    line.className = 'note';
    line.setAttribute('role', 'none');
    line.textContent = text;
    return line;
  }

  /**
   * Asks the instance once per video what it has, so the menu offers no
   * resolution the video lacks. A plain click needs no answer.
   */
  function askFormats() {
    if (offered?.url === page.url) return;
    const asked = { url: page.url, answer: null };
    offered = asked;
    void chrome.runtime
      .sendMessage({ type: 'knightloader-video-formats', href: location.href })
      .catch(() => null)
      .then((answer) => {
        asked.answer = Array.isArray(answer?.formats) ? answer : { formats: ['video'], unknown: true };
        if (offered === asked && !menu.hidden) renderMenu();
      });
  }

  function renderMenu() {
    const items = [];
    if (page.playlist) {
      const whole = menuItem(labels.playlist, 'menuitemcheckbox', () => {
        playlist = !playlist;
        whole.setAttribute('aria-checked', String(playlist));
      });
      whole.setAttribute('aria-checked', String(playlist));
      items.push(whole, separator());
    }
    const answer = offered?.answer;
    menu.setAttribute('aria-busy', String(!answer));
    if (!answer) items.push(note(labels.checking));
    for (const format of answer?.formats ?? []) {
      if (!VIDEO_FORMATS.includes(format)) continue;
      const text = format === 'video' ? labels.best : format === 'audio' ? labels.audio : format;
      items.push(menuItem(text, 'menuitem', () => void sendVideo(format)));
    }
    if (answer?.unknown) items.push(note(labels.unknown));
    const site = VIDEO_SITES.find((s) => s.id === page.site);
    items.push(
      separator(),
      menuItem(labels.hide.replace('{site}', site.name), 'menuitem', () => {
        chrome.runtime.sendMessage({ type: 'knightloader-video-hide' }).catch(() => {});
      }),
    );
    menu.replaceChildren(...items);
    menu.querySelectorAll('button')[0]?.focus();
  }

  function openMenu() {
    askFormats();
    renderMenu();
    menu.hidden = false;
    more.setAttribute('aria-expanded', 'true');
    position();
  }

  function closeMenu() {
    if (menu.hidden) return;
    menu.hidden = true;
    more.setAttribute('aria-expanded', 'false');
  }

  send.addEventListener('click', (e) => {
    if (e.isTrusted) void sendVideo('');
  });
  more.addEventListener('click', (e) => {
    if (!e.isTrusted) return;
    if (menu.hidden) openMenu();
    else closeMenu();
  });
  menu.addEventListener('keydown', (e) => {
    const items = [...menu.querySelectorAll('button')];
    const at = items.indexOf(root.activeElement);
    if (e.key === 'Escape') {
      closeMenu();
      more.focus();
    } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      items[(at + (e.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length]?.focus();
    }
  });

  // The player listens for clicks and keys too: a click on the button must not
  // also pause the video, a double click must not toggle fullscreen, and a
  // space or an arrow key pressed in the menu must not seek.
  for (const type of ['click', 'dblclick', 'mousedown', 'mouseup', 'pointerdown', 'pointerup', 'touchstart', 'touchend', 'keydown', 'keyup', 'wheel', 'contextmenu']) {
    host.addEventListener(type, (e) => e.stopPropagation());
  }
  host.addEventListener('pointerenter', () => {
    hovering = true;
    position();
  });
  host.addEventListener('pointerleave', () => {
    hovering = false;
    pointerAt = performance.now();
  });

  document.addEventListener(
    'pointermove',
    (e) => {
      if (!video || !host.isConnected) return;
      const r = video.getBoundingClientRect();
      if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) return;
      pointerAt = performance.now();
      position();
    },
    { capture: true, passive: true },
  );
  document.addEventListener(
    'pointerdown',
    (e) => {
      if (!e.composedPath().includes(host)) closeMenu();
    },
    { capture: true },
  );
  document.addEventListener('fullscreenchange', update);
  // YouTube moves between videos without loading a page, and so do the other
  // sites; the address and the video element are checked again every second
  // as well, for the moves no event announces.
  document.addEventListener('yt-navigate-finish', update);
  window.addEventListener('popstate', update);
  setInterval(update, 1000);
  matchMedia('(prefers-color-scheme: light)').addEventListener('change', update);

  async function loadLabels() {
    labels = await chrome.runtime.sendMessage({ type: 'knightloader-video-labels' }).catch(() => null);
  }

  // Switching the button off, or hiding it on this site, takes it away at once
  // in every tab that has it, and switching it on brings it back.
  chrome.storage.onChanged.addListener(async (changes, area) => {
    if (area !== 'local') return;
    for (const k of STORED) if (changes[k]) stored = { ...stored, [k]: changes[k].newValue };
    if (changes.language) await loadLabels();
    update();
  });

  void (async () => {
    stored = await chrome.storage.local.get(STORED);
    await loadLabels();
    update();
  })();
})();

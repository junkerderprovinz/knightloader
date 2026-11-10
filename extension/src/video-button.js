// The download button on a video's player, running inside the site; video.js
// has the rules and video-worker.js does the sending. It floats over the
// middle of the video's top edge, between the corners and the bottom bar where
// players keep their own controls, until it is dragged somewhere else in the
// player, and each site keeps that place. With auto-hide on it shows while
// the pointer moves over the video, as those controls do.
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
  // How far the pointer moves on the button before a press is a drag rather
  // than a click.
  const DRAG_PX = 5;
  const STORED = ['videoButton', 'videoButtonHidden', 'videoButtonAutoHide', 'videoButtonSpot', 'videoDefaults', 'videoSeen', 'theme', 'accent', 'shape'];
  // What sendVideo in video-worker.js answers, each with its label, and the
  // two that went through.
  const RESULTS = ['sent', 'already', 'held', 'refused', 'offline', 'unpaired', 'failed'];
  const SUCCESS = ['sent', 'already'];
  const SVG = 'http://www.w3.org/2000/svg';
  const CARET = 'M1.4 1.4 L8.6 1.4 L5 4.6 Z';
  const STAR = 'M5 0 L6.2 3.6 L10 3.8 L7 6.1 L8.1 10 L5 7.8 L1.9 10 L3 6.1 L0 3.8 L3.8 3.6 Z';
  // GlimStone's quick out curve, which settles without a bounce.
  const EASE_OUT = 'cubic-bezier(0.16, 1, 0.3, 1)';
  const RADIUS = { round: '9999px', soft: '5px', square: '0px', leaf: '18px' };
  // What a player draws over its video for people to use, whatever the site
  // calls it.
  const CONTROLS = 'a, button, input, select, textarea, [role="button"], [role="link"], [role="slider"], [role="checkbox"], [role="menuitem"]';

  // GlimStone's tokens and its subtle motion level, which the extension's own
  // pages run at, since glimstone.css does not reach into a shadow root.
  const STYLE = `
    :host { all: initial; position: fixed; z-index: 2147483647; translate: -50% -50%; }
    .kl {
      --surface: #262626; --raised: #393939; --text: #f4f4f4; --sub: #c6c6c6;
      --ok: #6fdc8c; --fail: #ff8389; --line: rgba(255, 255, 255, .08); --edge: rgba(255, 255, 255, .16);
      --elevation: 0 1px 2px rgba(0, 0, 0, .40), 0 18px 40px -24px rgba(0, 0, 0, .85);
      position: relative;
      font: 500 14px/1.4 "Noto Sans Variable", "Noto Sans", system-ui, sans-serif;
      color: var(--text);
      opacity: 0;
      pointer-events: none;
      transition: opacity 140ms ease;
    }
    .kl[data-theme="light"] {
      --surface: #ffffff; --raised: #e8e8e8; --text: #161616; --sub: #525252;
      --ok: #0e6027; --fail: #da1e28; --line: rgba(0, 0, 0, .08); --edge: rgba(0, 0, 0, .22);
      --elevation: 0 1px 2px rgba(0, 0, 0, .10), 0 16px 36px -26px rgba(0, 0, 0, .45);
    }
    .kl.shown { opacity: 1; pointer-events: auto; }
    /* The edge keeps the light button apart from a bright frame, and shows
       between its two halves. */
    .bar {
      display: flex; gap: 1px;
      background: var(--edge);
      border-radius: var(--radius); box-shadow: var(--elevation), 0 0 0 1px var(--edge);
      touch-action: none; user-select: none;
    }
    .kl.dragging, .kl.dragging button { cursor: grabbing; }
    button {
      all: unset;
      box-sizing: border-box;
      display: inline-flex; align-items: center; justify-content: center; gap: 8px;
      height: 32px;
      cursor: pointer;
      white-space: nowrap;
      transition: background-color 120ms ease, color 120ms ease;
    }
    button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
    .send {
      padding: 0 14px 0 10px;
      background: var(--surface); color: var(--text);
      border-start-start-radius: var(--radius); border-end-start-radius: var(--radius);
    }
    .mark { display: block; flex: 0 0 auto; width: 13px; height: 20px; }
    .mark * { transition: fill 120ms ease; }
    .more {
      width: 32px;
      background: var(--surface); color: var(--sub);
      border-start-end-radius: var(--radius); border-end-end-radius: var(--radius);
    }
    /* Gold under the pointer and the keyboard, the colour a success flashes
       in too. The mark takes the ink, since its own gold would vanish. */
    .send:hover, .send:focus-visible, .more:hover, .more:focus-visible, .more[aria-expanded="true"] {
      background: var(--accent); color: var(--accent-contrast);
    }
    .send:hover .mark *, .send:focus-visible .mark * { fill: currentColor; }
    .kl[data-state="sending"] .send { animation: pulse 3.2s ease-in-out infinite; }
    .kl[data-state="sent"] .send { color: var(--ok); }
    .kl[data-state="already"] .send { color: var(--text); }
    .kl[data-state="held"] .send, .kl[data-state="refused"] .send, .kl[data-state="offline"] .send,
    .kl[data-state="unpaired"] .send, .kl[data-state="failed"] .send { color: var(--fail); }
    .kl[data-state] .send:hover, .kl[data-state] .send:focus-visible { color: var(--accent-contrast); }
    .burst { position: absolute; inset: 0; overflow: visible; pointer-events: none; }
    /* The outline keeps a pale star readable on a bright frame. */
    .star { position: absolute; display: block; pointer-events: none; filter: drop-shadow(0 0 1px rgba(0, 0, 0, .45)); }
    .menu {
      position: absolute; top: calc(100% + 8px); left: 50%; translate: -50% 0;
      min-width: 220px; max-width: 290px; max-height: min(70vh, 460px); overflow-y: auto; overscroll-behavior: contain;
      padding: 4px;
      display: flex; flex-direction: column;
      background: var(--surface);
      border-radius: min(var(--radius), 10px);
      box-shadow: var(--elevation), inset 0 0 0 1px var(--line);
    }
    .menu.up { top: auto; bottom: calc(100% + 8px); }
    .menu[data-side="start"] { left: 0; translate: 0 0; }
    .menu[data-side="end"] { left: auto; right: 0; translate: 0 0; }
    .menu[hidden] { display: none; }
    .menu button { justify-content: flex-start; flex: 0 0 auto; height: 30px; padding: 0 10px; border-radius: min(var(--radius), 6px); color: var(--text); }
    .menu button:hover, .menu button:focus-visible { background: var(--raised); color: var(--text); outline: none; }
    .menu [role="menuitemcheckbox"]::after { content: ""; margin-inline-start: auto; width: 14px; }
    .menu [role="menuitemcheckbox"][aria-checked="true"]::after { content: "\\2713"; color: var(--accent); }
    .menu [role="separator"] { flex: 0 0 auto; height: 1px; margin: 4px 6px; background: var(--line); }
    .menu .chips { display: flex; flex-wrap: wrap; gap: 4px; padding: 0 6px 6px; }
    .menu .chips button { height: 26px; padding: 0 10px; border-radius: min(var(--radius), 13px); background: var(--raised); color: var(--sub); }
    .menu .chips button[role]::after { content: none; }
    .menu .chips button:hover, .menu .chips button:focus-visible { color: var(--text); }
    .menu .chips button[aria-checked="true"] { background: var(--accent); color: var(--accent-contrast); }
    .menu .go { justify-content: center; margin: 2px 0; background: var(--accent); color: var(--accent-contrast); }
    .menu .go:hover, .menu .go:focus-visible { background: color-mix(in srgb, var(--accent) 85%, var(--text)); color: var(--accent-contrast); }
    .note { display: flex; flex: 0 0 auto; align-items: center; min-height: 24px; padding: 0 10px; color: var(--sub); font-size: 12px; }
    .menu[aria-busy="true"] .note.busy { animation: pulse 3.2s ease-in-out infinite; }
    @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: .45; } }
    @keyframes shake {
      0%, 100% { transform: translateX(0); } 20% { transform: translateX(-4px); } 40% { transform: translateX(4px); }
      60% { transform: translateX(-2px); } 80% { transform: translateX(2px); }
    }
    @keyframes shake-reduced { 0%, 100% { opacity: 1; } 50% { opacity: .5; } }
    @media (prefers-reduced-motion: no-preference) {
      .send.shake { animation: shake 220ms ease-in-out; }
    }
    @media (prefers-reduced-motion: reduce) {
      .kl, button, .mark * { transition: none; }
      .kl[data-state="sending"] .send, .menu[aria-busy="true"] .note.busy { animation: none; }
      .send.shake { animation: shake-reduced 220ms ease-in-out; }
    }
  `;

  let stored = {};
  let labels = null;
  let page = null;
  let video = null;
  // When the pointer last moved over the video, 0 once it left.
  let pointerAt = 0;
  let hovering = false;
  let focused = false;
  let following = 0;
  let drop = 12;
  let playlist = false;
  let sending = false;
  let result = null;
  let resultTimer = 0;
  // What the instance said the video has: the address asked about, and its
  // answer once there is one.
  let offered = null;
  // The parts and qualities the open menu sends, starting from the site's
  // defaults.
  let pick = null;
  // A press on the bar, which becomes a drag once the pointer moves far
  // enough, and where the button's middle is while it does.
  let press = null;
  let dragAt = null;
  // Set when a drag ends, so the click the browser fires after it sends
  // nothing.
  let dragged = false;
  // The look last drawn, whose colours a success flashes in, and the
  // success's animations while they run.
  let look = null;
  let cheer = [];
  let cheerUntil = 0;

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
  send.append(sendLabel);
  let markOf = '';
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
  // The stars fly in a layer of their own over the button, which neither clips
  // them nor catches the pointer.
  const burst = document.createElement('div');
  burst.className = 'burst';
  frame.append(bar, burst, menu);
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

  /** Puts the extension's mark in front of the label. It comes as markup with
   *  the labels and is drawn inline, which no policy of the site's blocks. */
  function showMark(markup) {
    if (markup === markOf) return;
    markOf = markup;
    send.querySelector('.mark')?.remove();
    const svg = document.importNode(new DOMParser().parseFromString(markup, 'image/svg+xml').documentElement, true);
    svg.removeAttribute('id');
    svg.setAttribute('class', 'mark');
    svg.setAttribute('aria-hidden', 'true');
    send.prepend(svg);
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
   * How far below the video's top edge the button sits where it has no place
   * of its own: the first place, from 12px down, where it covers none of the
   * player's controls. Embedded players keep a title and buttons along the
   * top, a watch page keeps none.
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
    const wanted = page && labels && videoButtonWanted(stored, page.site);
    const found = wanted ? largestVideo() : null;
    // Players let go of their video for a moment, between an advert and the
    // video or while they lay themselves out again. Somebody in the menu or
    // waiting for a send keeps the button where it is until the video is back.
    if (wanted && !found && host.isConnected && (!menu.hidden || sending)) return;
    video = found;
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

  function barSize() {
    return { width: bar.offsetWidth, height: bar.offsetHeight };
  }

  /** Where the button's middle goes on the video: its place for the site, or
   *  the middle of the top edge, below the player's own controls. */
  function placeOnVideo(r) {
    const spot = videoSpotFor(stored, page.site);
    if (spot) return videoSpotPoint(spot, r, barSize());
    return { x: r.left + r.width / 2, y: r.top + drop + barSize().height / 2 };
  }

  /**
   * Follows the video and decides whether the button shows. While somebody is
   * at it, it follows a frame at a time; a button that only sits there
   * follows scrolling and the check every second instead, since each frame
   * would cost a hit test.
   */
  function position() {
    if (!video || !host.isConnected) return;
    const r = video.getBoundingClientRect();
    const at = dragAt ?? placeOnVideo(r);
    host.style.setProperty('left', `${Math.round(at.x)}px`);
    host.style.setProperty('top', `${Math.round(at.y)}px`);
    menu.classList.toggle('up', at.y > r.top + r.height / 2);
    // Near a side of the player the menu opens along it, not over its edge.
    const room = 150;
    menu.dataset.side = at.x - r.left < room ? 'start' : r.right - at.x < room ? 'end' : '';
    const state = {
      autoHide: stored.videoButtonAutoHide === true,
      pointerOver: pointerAt > 0 && performance.now() - pointerAt < IDLE_MS,
      hovering,
      focused,
      menuOpen: !menu.hidden,
      dragging: dragAt !== null,
      busy: sending || result !== null || performance.now() < cheerUntil,
    };
    // A menu or a send in hand stays in view even where the spot under the
    // button reads as something of the site's.
    const held = hovering || state.dragging || state.menuOpen || state.busy;
    const shown = videoButtonShows(state) && (held || onPlayer(at.x, at.y));
    frame.classList.toggle('shown', shown);
    const lively = state.pointerOver || hovering || focused || state.menuOpen || state.dragging || state.busy;
    if (shown && lively && !following) {
      following = requestAnimationFrame(() => {
        following = 0;
        position();
      });
    }
  }

  function render() {
    look = lookOf(stored);
    frame.dataset.theme = look.theme;
    frame.style.setProperty('--accent', look.accent);
    frame.style.setProperty('--accent-contrast', look.ink);
    frame.style.setProperty('--radius', look.radius);
    frame.dir = labels.dir;
    frame.lang = labels.lang;
    frame.dataset.state = sending ? 'sending' : (result?.result ?? '');
    showMark(labels.mark);
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

  async function sendVideo(chosen) {
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
      .sendMessage({ type: 'knightloader-video-send', href: location.href, pick: chosen, playlist })
      .catch(() => null);
    sending = false;
    result = RESULTS.includes(answer?.result) ? answer : { result: 'failed' };
    update();
    if (SUCCESS.includes(result.result)) celebrate();
    else replay(send, 'shake');
    resultTimer = setTimeout(() => {
      result = null;
      update();
    }, RESULT_MS);
  }

  /**
   * The cheer of a send that went through: a quick pulse of the button in
   * gold and a few stars flying off it. With reduced motion only the colour
   * flashes. A second one ends the first rather than piling up.
   */
  function celebrate() {
    for (const a of cheer) a.cancel();
    burst.replaceChildren();
    const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
    const flash = [{ backgroundColor: look.accent, color: look.ink }, {}];
    cheer = [send.animate(flash, { duration: 320, easing: EASE_OUT })];
    cheerUntil = performance.now() + (reduced ? 320 : 700);
    if (!reduced) {
      cheer.push(send.animate([{ transform: 'scale(1)' }, { transform: 'scale(1.08)', offset: 0.35 }, { transform: 'scale(1)' }], { duration: 320, easing: EASE_OUT }));
      const x = send.offsetLeft + send.offsetWidth / 2;
      const y = send.offsetTop + send.offsetHeight / 2;
      const count = 5 + Math.floor(Math.random() * 3);
      for (let i = 0; i < count; i++) {
        const turn = ((i + Math.random() * 0.5 - 0.25) / count) * 2 * Math.PI;
        const reach = 24 + Math.random() * 24;
        const size = 6 + Math.round(Math.random() * 4);
        const star = icon(STAR, '0 0 10 10', size);
        star.setAttribute('class', 'star');
        star.style.setProperty('left', `${x}px`);
        star.style.setProperty('top', `${y}px`);
        star.style.setProperty('color', i % 2 ? `color-mix(in srgb, ${look.accent} 55%, #fff)` : look.accent);
        burst.append(star);
        const dx = Math.cos(turn) * reach;
        const dy = Math.sin(turn) * reach;
        const spin = Math.round(Math.random() * 120 - 60);
        // Out fast and whole, then fading as it slows, so the star is seen in
        // flight and not only at its start.
        const fly = star.animate(
          [
            { transform: 'translate(-50%, -50%) rotate(0deg) scale(1)', opacity: 1, easing: EASE_OUT },
            { transform: `translate(-50%, -50%) translate(${dx * 0.8}px, ${dy * 0.8}px) rotate(${spin * 0.8}deg) scale(.8)`, opacity: 1, offset: 0.55, easing: 'ease-in' },
            { transform: `translate(-50%, -50%) translate(${dx}px, ${dy}px) rotate(${spin}deg) scale(.3)`, opacity: 0 },
          ],
          { duration: 600 + Math.round(Math.random() * 100), delay: Math.round(Math.random() * 40), fill: 'forwards' },
        );
        fly.finished.then(() => star.remove(), () => {});
        cheer.push(fly);
      }
    }
    position();
  }

  /** Plays a one-shot animation again, as replay in shared.js does. */
  function replay(el, cls) {
    el.classList.remove('shake');
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

  function checkItem(text, checked, onPick) {
    const item = menuItem(text, 'menuitemcheckbox', onPick);
    item.setAttribute('aria-checked', String(checked));
    return item;
  }

  /** A row of choices, of which one is picked, or with `isOn` any number. */
  function chips(values, isOn, name, onPick, several = false) {
    const row = document.createElement('div');
    row.className = 'chips';
    row.setAttribute('role', 'group');
    row.append(...values.map((v) => checkItem(name(v), isOn(v), () => onPick(v))));
    if (!several) for (const item of row.children) item.setAttribute('role', 'menuitemradio');
    return row;
  }

  function separator() {
    const line = document.createElement('div');
    line.setAttribute('role', 'separator');
    return line;
  }

  function note(text, busy = false) {
    const line = document.createElement('div');
    line.className = busy ? 'note busy' : 'note';
    line.setAttribute('role', 'none');
    line.textContent = text;
    return line;
  }

  /**
   * Asks the instance once per video what it has, so the menu offers nothing
   * the video lacks. A plain click needs no answer. An answer that comes
   * while the menu is open makes its pick exact for the video.
   */
  function askFormats() {
    if (offered?.url === page.url) return;
    const asked = { url: page.url, answer: null };
    offered = asked;
    void chrome.runtime
      .sendMessage({ type: 'knightloader-video-formats', href: location.href })
      .catch(() => null)
      .then((answer) => {
        asked.answer = typeof answer?.known === 'boolean' ? answer : videoOffer(null);
        if (offered !== asked || menu.hidden) return;
        pick = videoResolve(pick, asked.answer);
        renderMenu();
      });
  }

  /** The resolutions the menu offers where the instance lists no tracks: the
   *  video's own, or where it could not say, those found on the site. The
   *  site's default is among them either way, since a cap the video does not
   *  reach downloads its best. */
  function menuQualities(offer) {
    const own = offer.qualities.length ? offer.qualities : videoSiteSeen(stored, page.site).qualities;
    const wanted = videoDefaultsFor(stored, page.site).quality;
    return VIDEO_CAPS.filter((q) => own.includes(q) || q === wanted);
  }

  function togglePart(part) {
    const on = pick.parts.includes(part);
    // The last part stays, since a download of nothing is no download.
    if (on && pick.parts.length === 1) return;
    pick.parts = VIDEO_PARTS.filter((p) => (p === part ? !on : pick.parts.includes(p)));
  }

  /** A video track by its quality, with its container and codec where the
   *  video has that quality in more than one. */
  function trackName(key, tracks) {
    const quality = key.split(' ')[0];
    return tracks.filter((k) => k.split(' ')[0] === quality).length > 1 ? key : quality;
  }

  /** The choices under a ticked part: what this video has of it. */
  function partChoices(part, offer, redraw) {
    const best = (v) => (v === '' ? labels.best : v);
    switch (part) {
      case 'video': {
        if (!offer.known || offer.videoTracks.length === 0) {
          return [chips(['best', ...menuQualities(offer)], (q) => q === pick.quality, (q) => (q === 'best' ? labels.best : q), (q) => redraw(() => (pick.quality = q)))];
        }
        const tracks = offer.videoTracks;
        return [chips(['', ...tracks], (k) => k === (pick.videoTrack ?? ''), (k) => (k ? trackName(k, tracks) : labels.best), (k) => redraw(() => (pick.videoTrack = k || undefined)))];
      }
      case 'audio': {
        const out = [];
        if (offer.audioTracks.length) {
          out.push(chips(['', ...offer.audioTracks], (k) => k === (pick.audioTrack ?? ''), best, (k) => redraw(() => (pick.audioTrack = k || undefined))));
        }
        if (offer.audioLangs.length > 1) {
          const name = (code) => {
            const lang = offer.audioLangs.find((l) => l.code === code);
            return lang.dubbed ? `${code} (${labels.dubbed})` : code;
          };
          out.push(note(labels.language), chips(offer.audioLangs.map((l) => l.code), (c) => c === pick.audioLang, name, (c) => redraw(() => (pick.audioLang = c))));
        }
        return out;
      }
      case 'subtitle': {
        const { manual, auto } = videoSubtitleChoices(offer, pick);
        const toggle = (code) =>
          redraw(() => {
            pick.subLangs = pick.subLangs.includes(code) ? pick.subLangs.filter((c) => c !== code) : [...pick.subLangs, code];
            pick.subAuto = pick.subLangs.some((c) => auto.includes(c) && !manual.includes(c));
          });
        const isOn = (code) => pick.subLangs.includes(code);
        const out = [];
        if (manual.length) out.push(chips(manual, isOn, (c) => c, toggle, true));
        if (auto.length) out.push(note(labels.autoCaptions), chips(auto, isOn, (c) => c.replace(/-orig$/, ''), toggle, true));
        if (offer.subtitleFormats.length) {
          const name = (f) => (f === 'best' ? labels.best : f);
          out.push(note(labels.subFormat), chips(['best', ...offer.subtitleFormats], (f) => f === pick.subFormat, name, (f) => redraw(() => (pick.subFormat = f))));
        }
        return out;
      }
      case 'thumbnail': {
        const th = offer.thumbnail;
        return th ? [note(th.width && th.height ? `${th.width}\u00d7${th.height} ${th.ext}` : th.ext)] : [];
      }
      default:
        return [];
    }
  }

  /** Draws the menu for the current pick, keeping the keyboard on the item
   *  it was on. */
  function renderMenu() {
    const focusAt = [...menu.querySelectorAll('button')].indexOf(root.activeElement);
    const redraw = (change) => {
      change();
      renderMenu();
    };
    const offer = offered?.answer;
    const items = [];
    if (page.playlist) {
      items.push(checkItem(labels.playlist, playlist, () => redraw(() => (playlist = !playlist))), separator());
    }
    for (const part of VIDEO_PARTS) {
      if (offer && !videoHas(offer, part)) continue;
      items.push(checkItem(labels.parts[part], pick.parts.includes(part), () => redraw(() => togglePart(part))));
      if (offer && pick.parts.includes(part)) items.push(...partChoices(part, offer, redraw));
    }
    menu.setAttribute('aria-busy', String(!offer));
    if (!offer) items.push(note(labels.checking, true));
    else if (!offer.known) items.push(note(labels.unknown));
    const go = menuItem(labels.send, 'menuitem', () => void sendVideo(pick));
    go.classList.add('go');
    const site = VIDEO_SITES.find((s) => s.id === page.site);
    items.push(
      separator(),
      go,
      menuItem(labels.hide.replace('{site}', site.name), 'menuitem', () => {
        chrome.runtime.sendMessage({ type: 'knightloader-video-hide' }).catch(() => {});
      }),
    );
    menu.replaceChildren(...items);
    const buttons = menu.querySelectorAll('button');
    (buttons[focusAt] ?? buttons[0])?.focus();
  }

  function openMenu() {
    pick = videoDefaultsFor(stored, page.site);
    askFormats();
    if (offered.answer) pick = videoResolve(pick, offered.answer);
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
    if (e.isTrusted) void sendVideo(videoDefaultsFor(stored, page.site));
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

  // A press on the bar that moves becomes a drag, which places the button
  // anywhere on the video and keeps that place for the site.
  bar.addEventListener('pointerdown', (e) => {
    if (!e.isTrusted || e.button !== 0) return;
    dragged = false;
    press = { id: e.pointerId, x: e.clientX, y: e.clientY, moving: false };
  });
  bar.addEventListener('pointermove', (e) => {
    if (!press || e.pointerId !== press.id || !video) return;
    if (!press.moving) {
      if (Math.hypot(e.clientX - press.x, e.clientY - press.y) < DRAG_PX) return;
      press.moving = true;
      const box = host.getBoundingClientRect();
      press.dx = box.left + box.width / 2 - press.x;
      press.dy = box.top + box.height / 2 - press.y;
      bar.setPointerCapture(e.pointerId);
      frame.classList.add('dragging');
      closeMenu();
    }
    const r = video.getBoundingClientRect();
    const share = { x: (e.clientX + press.dx - r.left) / r.width, y: (e.clientY + press.dy - r.top) / r.height };
    dragAt = videoSpotPoint(share, r, barSize());
    position();
  });
  bar.addEventListener('pointerup', (e) => {
    if (!press || e.pointerId !== press.id) return;
    if (press.moving) {
      dragged = true;
      const spot = videoSpotOf(dragAt, video.getBoundingClientRect(), barSize());
      // Kept here at once, so the button does not jump back while the
      // storage catches up.
      stored = { ...stored, videoButtonSpot: { ...stored.videoButtonSpot, [page.site]: spot } };
      void chrome.storage.local.set({ videoButtonSpot: stored.videoButtonSpot });
    }
    endDrag();
  });
  bar.addEventListener('pointercancel', endDrag);
  bar.addEventListener(
    'click',
    (e) => {
      if (!dragged) return;
      dragged = false;
      e.stopPropagation();
    },
    true,
  );

  function endDrag() {
    press = null;
    dragAt = null;
    frame.classList.remove('dragging');
    position();
  }

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
  root.addEventListener('focusin', () => {
    focused = true;
    position();
  });
  // Focus moving from one item to the next leaves and comes back, so the
  // answer waits for where it lands.
  root.addEventListener('focusout', () => {
    setTimeout(() => {
      focused = root.activeElement !== null;
      position();
    });
  });

  document.addEventListener(
    'pointermove',
    (e) => {
      if (!video || !host.isConnected) return;
      const r = video.getBoundingClientRect();
      const over = e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom;
      if (!over && pointerAt === 0) return;
      pointerAt = over ? performance.now() : 0;
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
  document.addEventListener('scroll', position, { capture: true, passive: true });
  window.addEventListener('resize', position);
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
  // in every tab that has it, and switching it on brings it back. A place
  // reset in the options takes it back to the top.
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

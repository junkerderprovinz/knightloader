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
  // How long "Done" or a failed download stays on the button.
  const END_MS = 20000;
  // How often the button asks about the rows it follows, and less often once
  // a download has run for a minute.
  const POLL_MS = 3000;
  const POLL_SLOW_MS = 10000;
  const POLL_SLOW_AFTER = 60000;
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
  // GlimStone's control radius per shape. Round and leaf stop at 12px, since a
  // pressable thing is rounded but never a pill.
  const RADIUS = { round: '12px', soft: '5px', square: '0px', leaf: '12px' };
  // What a player draws over its video for people to use, whatever the site
  // calls it.
  const CONTROLS = 'a, button, input, select, textarea, [role="button"], [role="link"], [role="slider"], [role="checkbox"], [role="menuitem"]';

  // GlimStone's tokens and its subtle motion level, which the extension's own
  // pages run at, since glimstone.css does not reach into a shadow root.
  const STYLE = `
    :host { all: initial; position: fixed; z-index: 2147483647; translate: -50% -50%; }
    .kl {
      --surface: #262626; --surface2: #393939; --surface3: #525252; --raised: #6f6f6f; --text: #f4f4f4; --sub: #c6c6c6;
      --ok: #6fdc8c; --fail: #ff8389; --line: rgba(255, 255, 255, .08);
      --knob: #c6c6c6; --track: rgba(255, 255, 255, .16); --hover: rgba(255, 255, 255, .05);
      --elevation: 0 1px 2px rgba(0, 0, 0, .40), 0 18px 40px -24px rgba(0, 0, 0, .85);
      position: relative;
      font: 500 14px/1.4 "Noto Sans Variable", "Noto Sans", system-ui, sans-serif;
      color: var(--text);
      opacity: 0;
      pointer-events: none;
      transition: opacity 140ms ease;
    }
    .kl[data-theme="light"] {
      --surface: #ffffff; --surface2: #e8e8e8; --surface3: #d1d1d1; --raised: #c6c6c6; --text: #161616; --sub: #525252;
      --ok: #0e6027; --fail: #da1e28; --line: rgba(0, 0, 0, .08);
      --knob: #6f6f6f; --track: rgba(0, 0, 0, .12); --hover: rgba(0, 0, 0, .04);
      --elevation: 0 1px 2px rgba(0, 0, 0, .10), 0 16px 36px -26px rgba(0, 0, 0, .45);
    }
    .kl.shown { opacity: 1; pointer-events: auto; }
    /* The faint line shows only between the two halves. */
    .bar {
      display: flex; gap: 1px;
      background: var(--line);
      border-radius: var(--radius); box-shadow: var(--elevation);
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
      position: relative;
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
    /* Centred on the mark, which is 13px wide behind 10px of padding. */
    .ring {
      position: absolute; inset-inline-start: 3.5px; top: 3px; width: 26px; height: 26px;
      display: none; overflow: visible; pointer-events: none; rotate: -90deg;
    }
    .kl[data-ring] .ring { display: block; }
    .ring circle { fill: none; stroke-width: 2; }
    .ring .track { stroke: color-mix(in srgb, var(--accent) 22%, transparent); }
    .ring .arc { stroke: var(--accent); stroke-linecap: round; transition: stroke-dasharray 400ms ease; }
    .send:hover .ring .arc, .send:focus-visible .ring .arc { stroke: currentColor; }
    .send:hover .ring .track, .send:focus-visible .ring .track { stroke: color-mix(in srgb, currentColor 30%, transparent); }
    .kl[data-ring="unknown"] .ring { animation: spin 2.4s linear infinite; }
    .kl[data-state="sending"] .send { animation: pulse 3.2s ease-in-out infinite; }
    .kl[data-state="sent"] .send, .kl[data-state="done"] .send { color: var(--ok); }
    .kl[data-state="already"] .send { color: var(--text); }
    .kl[data-state="held"] .send, .kl[data-state="refused"] .send, .kl[data-state="offline"] .send,
    .kl[data-state="unpaired"] .send, .kl[data-state="failed"] .send, .kl[data-state="loadFailed"] .send { color: var(--fail); }
    .kl[data-state] .send:hover, .kl[data-state] .send:focus-visible { color: var(--accent-contrast); }
    .burst { position: absolute; inset: 0; overflow: visible; pointer-events: none; }
    /* The outline keeps a pale star readable on a bright frame. */
    .star { position: absolute; display: block; pointer-events: none; filter: drop-shadow(0 0 1px rgba(0, 0, 0, .45)); }
    /* As tall as the player leaves room for (position sets max-height), so
       the parts sit in order without scrolling on most videos. It is a
       window, and each part a sub-card on it one surface up. */
    .menu {
      position: absolute; top: calc(100% + 8px); left: 50%; translate: -50% 0;
      box-sizing: border-box; width: 340px; max-width: calc(100vw - 16px); max-height: 460px; overflow-y: auto; overscroll-behavior: contain;
      padding: 6px 6px 0;
      display: flex; flex-direction: column; gap: 6px;
      font-size: 13px; line-height: 1.35;
      background: var(--surface);
      border-radius: min(var(--radius), 10px);
      box-shadow: var(--elevation), inset 0 0 0 1px var(--line);
      scrollbar-width: thin; scrollbar-color: var(--track) transparent;
    }
    .menu.up { top: auto; bottom: calc(100% + 8px); }
    .menu[data-side="start"] { left: 0; translate: 0 0; }
    .menu[data-side="end"] { left: auto; right: 0; translate: 0 0; }
    .menu[hidden] { display: none; }
    .menu button { position: relative; justify-content: flex-start; height: auto; border-radius: min(var(--radius), 6px); color: var(--text); }
    /* The ring reaches over the neighbours, and its gap shows whatever the
       control sits on, so it reads on a tile and on gold alike. */
    .kl .menu button:focus-visible { z-index: 1; }
    .menu .card { position: relative; flex: 0 0 auto; background: var(--surface2); border-radius: min(var(--radius), 8px); }
    /* A part's name is its heading, the accent badge the extension's pages
       head a card with, filled whether the part is on or off. The switch's
       track at the end of the row belongs to the same control, so a click on
       either toggles the part. */
    .menu .switch {
      position: static; display: inline-block; box-sizing: border-box; vertical-align: top;
      max-width: calc(100% - 66px); height: 22px; margin: 7px 10px; padding: 2px 10px;
      overflow: hidden; text-overflow: ellipsis;
      background: var(--accent); color: var(--accent-contrast); border-radius: min(var(--radius), 6px);
      font-size: 12px; font-weight: 500; line-height: 18px; letter-spacing: 1.2px; text-transform: uppercase;
    }
    /* The track, and over it the knob that slides along, centred on the
       header row. */
    .switch::before, .switch::after { content: ""; position: absolute; top: 18px; translate: 0 -50%; }
    .switch::after {
      inset-inline-end: 8px; width: 28px; height: 16px; border-radius: 8px;
      background: var(--track); transition: background-color 150ms ease;
    }
    .switch::before {
      z-index: 1; inset-inline-end: 23px; width: 10px; height: 10px; border-radius: 50%;
      background: var(--knob); transition: inset-inline-end 180ms ${EASE_OUT}, background-color 150ms ease;
    }
    .switch[aria-checked="true"]::after { background: var(--accent); }
    .switch[aria-checked="true"]::before { inset-inline-end: 11px; background: var(--accent-contrast); }
    .choices { padding: 0 4px 10px; }
    .choices .label { display: block; margin: 0 6px 4px; color: var(--sub); font-size: 11px; }
    .choices > * + .label { margin-top: 10px; }
    /* Every list is one column of tiles, the same whether one or several can
       be picked, with the sizes lined up at the end. A tile rests one surface
       above its sub-card, which would swallow it otherwise, and the picked
       ones are filled. */
    .list { display: flex; flex-direction: column; gap: 2px; }
    .menu .list button { gap: 8px; height: 28px; padding-block: 0; padding-inline: 10px; background: var(--surface3); }
    .list button:not([aria-checked="true"]):hover { background: var(--raised); }
    .menu .list [aria-checked="true"] { background: var(--accent); color: var(--accent-contrast); }
    .list .aside { margin-inline-start: auto; color: var(--sub); font-size: 12px; font-weight: 400; font-variant-numeric: tabular-nums; }
    .list button:hover .aside, .list [aria-checked="true"] .aside { color: inherit; }
    /* The groove one surface below the sub-card around a row of formats,
       whose idle segments stay unfilled. */
    .segments {
      display: flex; flex-wrap: wrap; gap: 0.2rem; margin: 0 2px; padding: 0.2rem;
      border-radius: min(var(--radius), 7px); background: var(--surface3);
    }
    .menu .segments button {
      flex: 1 1 0; min-width: 52px; min-height: 30px; flex-direction: column; justify-content: center; gap: 0;
      padding: 6px; border-radius: min(var(--radius), 5px); font-size: 12.5px; line-height: 1.15;
    }
    .segments button:not([aria-checked="true"]):not(:disabled):hover { background: var(--raised); }
    .segments .aside { color: var(--sub); font-size: 10.5px; font-weight: 400; }
    .segments button:hover .aside { color: inherit; }
    .menu .segments [aria-checked="true"] { background: var(--accent); color: var(--accent-contrast); }
    .segments [aria-checked="true"] .aside { color: inherit; opacity: .7; }
    .menu .segments button:disabled { opacity: .4; cursor: default; }
    /* Download stays in view at the bottom whatever scrolls above. */
    .menu .foot {
      position: sticky; bottom: 0; z-index: 2; flex: 0 0 auto;
      display: flex; flex-direction: column;
      margin: 0 -6px; padding: 0 6px 6px;
      background: var(--surface);
    }
    .menu .go { justify-content: center; gap: 0; height: 34px; background: var(--accent); color: var(--accent-contrast); font-weight: 600; }
    .menu .go:hover { background: color-mix(in srgb, var(--accent) 85%, var(--text)); }
    .go .total { margin-inline-start: .35em; font-weight: 500; font-variant-numeric: tabular-nums; }
    .note { display: flex; flex: 0 0 auto; align-items: center; min-height: 30px; padding: 0 8px; color: var(--sub); font-size: 12px; }
    .choices .note { min-height: 0; padding: 0 6px; }
    .menu[aria-busy="true"] .note.busy { animation: pulse 3.2s ease-in-out infinite; }
    @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: .45; } }
    @keyframes spin { from { rotate: -90deg; } to { rotate: 270deg; } }
    @keyframes shake {
      0%, 100% { transform: translateX(0); } 20% { transform: translateX(-4px); } 40% { transform: translateX(4px); }
      60% { transform: translateX(-2px); } 80% { transform: translateX(2px); }
    }
    @keyframes shake-reduced { 0%, 100% { opacity: 1; } 50% { opacity: .5; } }
    @media (prefers-reduced-motion: no-preference) {
      .send.shake { animation: shake 220ms ease-in-out; }
    }
    @media (prefers-reduced-motion: reduce) {
      .kl, button, .mark *, .ring .arc, .switch::before, .switch::after { transition: none; }
      .kl[data-state="sending"] .send, .menu[aria-busy="true"] .note.busy, .kl[data-ring="unknown"] .ring { animation: none; }
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
  // While the popup asks which instance a send goes to.
  let asking = false;
  let result = null;
  let resultTimer = 0;
  // The rows of a send the instance is downloading, while the button follows
  // them: the instance and the ids to ask about, and the share downloaded
  // once it has answered, null while that is unknown.
  let watch = null;
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
  const ring = document.createElementNS(SVG, 'svg');
  ring.setAttribute('class', 'ring');
  ring.setAttribute('viewBox', '0 0 26 26');
  ring.setAttribute('aria-hidden', 'true');
  const arc = circle('arc');
  ring.append(circle('track'), arc);
  send.append(ring, sendLabel);
  let markOf = '';
  const more = document.createElement('button');
  more.type = 'button';
  more.className = 'more';
  more.setAttribute('aria-haspopup', 'dialog');
  more.setAttribute('aria-expanded', 'false');
  more.append(icon(CARET, '0 0 10 6', 10));
  bar.append(send, more);
  const menu = document.createElement('div');
  menu.className = 'menu';
  menu.setAttribute('role', 'dialog');
  menu.hidden = true;
  // The stars fly in a layer of their own over the button, which neither clips
  // them nor catches the pointer.
  const burst = document.createElement('div');
  burst.className = 'burst';
  frame.append(bar, burst, menu);
  root.append(style, frame);

  function circle(cls) {
    const c = document.createElementNS(SVG, 'circle');
    c.setAttribute('class', cls);
    c.setAttribute('cx', '13');
    c.setAttribute('cy', '13');
    c.setAttribute('r', '12');
    c.setAttribute('pathLength', '100');
    return c;
  }

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
      // The old video's download goes on without the button.
      unfollow();
    }
    page = next;
    const wanted = page && labels && videoButtonWanted(stored, page.site);
    if (labels && !wanted) unfollow();
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
    const up = at.y > r.top + r.height / 2;
    menu.classList.toggle('up', up);
    // Near a side of the player the menu opens along it, not over its edge.
    const room = 180;
    menu.dataset.side = at.x - r.left < room ? 'start' : r.right - at.x < room ? 'end' : '';
    // The menu takes the height the player leaves it above or below the
    // button, of the part on screen.
    const half = barSize().height / 2 + 8;
    const tall = up ? at.y - half - Math.max(r.top, 0) : Math.min(r.bottom, innerHeight) - at.y - half;
    menu.style.setProperty('max-height', `${Math.max(240, Math.floor(tall) - 8)}px`);
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
    const share = watch?.share;
    if (share === undefined) delete frame.dataset.ring;
    else frame.dataset.ring = share === null ? 'unknown' : 'share';
    arc.setAttribute('stroke-dasharray', typeof share === 'number' ? `${Math.round(share * 1000) / 10} 100` : '25 75');
    const going = share === undefined ? '' : share === null ? labels.downloading : `${labels.downloading} ${new Intl.NumberFormat(labels.lang, { style: 'percent' }).format(share)}`;
    const tip = result?.reason || (result?.result === 'done' ? labels.doneTip : result ? labels[result.result] : going || labels.sendTip);
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

  /** Sends what choose picks, which may first ask the instance what the
   *  video has. */
  async function sendVideo(choose) {
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
    const chosen = await choose();
    const answer = await chrome.runtime
      .sendMessage({ type: 'knightloader-video-send', href: location.href, pick: chosen, playlist })
      .catch(() => null);
    // With more than one instance the popup asks where to, and what became
    // of the send comes later in a message of its own.
    if (answer?.result === 'asking') {
      asking = true;
      return;
    }
    showResult(answer);
  }

  function showResult(answer) {
    sending = false;
    result = RESULTS.includes(answer?.result) ? answer : { result: 'failed' };
    if (result.watch) follow(result.watch);
    update();
    if (SUCCESS.includes(result.result)) celebrate();
    else replay(send, 'shake');
    resultTimer = setTimeout(() => {
      result = null;
      update();
    }, RESULT_MS);
  }

  /**
   * Follows the rows a send started on the instance, a new send's in place of
   * an older one's. The ring shows once the instance has said where they
   * stand.
   */
  function follow(found) {
    unfollow();
    watch = { instance: found.instance, ids: found.ids, share: undefined, since: performance.now(), timer: 0 };
    askLater(watch);
  }

  function unfollow() {
    if (watch) clearTimeout(watch.timer);
    watch = null;
  }

  function askLater(w) {
    const slow = performance.now() - w.since > POLL_SLOW_AFTER;
    w.timer = setTimeout(() => void askProgress(w), slow ? POLL_SLOW_MS : POLL_MS);
  }

  /** Asks where the rows stand, while the button is on the page. No answer
   *  leaves the ring as it was until the next time. */
  async function askProgress(w) {
    const answer = host.isConnected
      ? await chrome.runtime.sendMessage({ type: 'knightloader-video-progress', instance: w.instance, ids: w.ids }).catch(() => null)
      : null;
    if (watch !== w) return;
    switch (answer?.state) {
      case 'running':
        w.share = typeof answer.share === 'number' ? Math.min(1, Math.max(0, answer.share)) : null;
        break;
      case 'done':
        end({ result: 'done', instance: w.instance });
        return;
      case 'failed':
        end({ result: 'loadFailed', reason: answer.reason, instance: w.instance });
        replay(send, 'shake');
        return;
      case 'gone':
        unfollow();
        update();
        return;
    }
    update();
    askLater(w);
  }

  /** Shows how the download ended for a while, in place of whatever the
   *  button said. */
  function end(shown) {
    unfollow();
    clearTimeout(resultTimer);
    result = shown;
    update();
    resultTimer = setTimeout(() => {
      result = null;
      update();
    }, END_MS);
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

  function control(text, onPick) {
    const item = document.createElement('button');
    item.type = 'button';
    item.textContent = text;
    item.addEventListener('click', (e) => {
      if (!e.isTrusted) return;
      // The menu is drawn again after most picks, and the keyboard goes back
      // to whatever had it.
      item.focus();
      onPick();
    });
    return item;
  }

  function toggle(text, on, key, onPick) {
    const item = control(text, onPick);
    item.className = 'switch';
    item.setAttribute('role', 'switch');
    item.setAttribute('aria-checked', String(on));
    item.setAttribute('id', `kl-${key}`);
    item.dataset.key = key;
    return item;
  }

  /**
   * A row of choices under a part, as a list or as segments,
   * of which one is picked, or with `several` any number. A choice `off`
   * rules out stays in the row but takes no click. `aside` is what a choice
   * shows small beside its name: the size in a list, the container under the
   * codec in segments.
   */
  function choices(values, { look, key, name, isOn, onPick, several = false, off = () => false, aside = () => '' }) {
    const group = document.createElement('div');
    group.className = look;
    group.setAttribute('role', several ? 'group' : 'radiogroup');
    const items = values.map((v, i) => {
      const item = control(name(v), () => onPick(v));
      item.setAttribute('role', several ? 'checkbox' : 'radio');
      item.setAttribute('aria-checked', String(isOn(v)));
      item.dataset.key = `${key}:${v}`;
      if (off(v)) item.disabled = true;
      const extra = aside(v);
      if (extra) {
        const small = document.createElement('span');
        small.className = 'aside';
        small.textContent = extra;
        item.append(small);
        item.setAttribute('aria-label', `${name(v)}, ${extra}`);
      }
      // A radio group is one stop for Tab, its picked radio, and the arrow
      // keys pick within it, as with the browser's own radios.
      if (!several) {
        item.setAttribute('tabindex', isOn(v) ? '0' : '-1');
        item.addEventListener('keydown', (e) => {
          const step = arrowStep(e.key);
          if (!step) return;
          e.preventDefault();
          e.stopPropagation();
          const next = stepTo(values.length, i, step, (j) => off(values[j]));
          items[next].focus();
          onPick(values[next]);
        });
      }
      group.append(item);
      return item;
    });
    return group;
  }

  /** Which way an arrow key moves in a row of choices: down and forward to
   *  the next, up and back to the one before, as with the browser's own
   *  radios. */
  function arrowStep(key) {
    const forward = labels.dir === 'rtl' ? 'ArrowLeft' : 'ArrowRight';
    const back = labels.dir === 'rtl' ? 'ArrowRight' : 'ArrowLeft';
    if (key === 'ArrowDown' || key === forward) return 1;
    if (key === 'ArrowUp' || key === back) return -1;
    return 0;
  }

  /** Where a step from `at` lands among n choices, round the ends and past
   *  a choice `off`, which `at` itself never is. */
  function stepTo(n, at, step, off) {
    let i = (at + step + n) % n;
    while (off(i)) i = (i + step + n) % n;
    return i;
  }

  function note(text, busy = false) {
    const line = document.createElement('div');
    line.className = busy ? 'note busy' : 'note';
    line.textContent = text;
    return line;
  }

  /**
   * Asks the instance once per video what it has, so the menu offers nothing
   * the video lacks, and answers with it. A click needs no answer unless the
   * defaults name a format or a bitrate. An answer that comes while the menu
   * is open makes its pick exact for the video.
   */
  function askFormats() {
    if (offered?.url === page.url) return offered.ready;
    const asked = { url: page.url, answer: null };
    offered = asked;
    asked.ready = chrome.runtime
      .sendMessage({ type: 'knightloader-video-formats', href: location.href })
      .catch(() => null)
      .then((answer) => {
        asked.answer = typeof answer?.known === 'boolean' ? answer : videoOffer(null);
        if (offered === asked && !menu.hidden) {
          pick = videoResolve(pick, asked.answer);
          renderMenu();
        }
        return asked.answer;
      });
    return asked.ready;
  }

  /** What a click sends: the site's defaults, made exact for the video where
   *  they name a format or a bitrate, which only one video's tracks have. */
  async function clickPick() {
    const defaults = videoDefaultsFor(stored, page.site);
    if (defaults.videoFormat === 'best' && defaults.audioRate === 'best') return defaults;
    const offer = await askFormats();
    return videoExact(videoResolve(defaults, offer), offer);
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

  /** The choices under a ticked part: what this video has of it, as rows of
   *  their own for its quality and its format, each with its label above it
   *  and named by the part and the label together. */
  function partChoices(part, offer, redraw) {
    const best = (name) => (v) => (v === 'best' ? labels.best : name(v));
    const size = (n) => (n ? videoSize(n, labels.lang) : '');
    const nodes = [];
    const row = (key, text, values, opts) => {
      const label = document.createElement('span');
      label.className = 'label';
      label.setAttribute('id', `kl-${part}-${key}`);
      label.textContent = text;
      const group = choices(values, { key: `${part}-${key}`, ...opts });
      group.setAttribute('aria-labelledby', `kl-${part} kl-${part}-${key}`);
      nodes.push(label, group);
    };
    switch (part) {
      case 'video': {
        if (!offer.known || offer.videoTracks.length === 0) {
          row('quality', labels.quality, ['best', ...menuQualities(offer)], { look: 'list', isOn: (q) => q === pick.quality, name: best((q) => q), onPick: (q) => redraw(() => (pick.quality = q)) });
          break;
        }
        const { qualities, formats } = videoMenuRows(offer);
        // A quality without the format picked takes the best format there,
        // and the row shows it.
        const quality = (q) =>
          redraw(() => {
            pick.quality = q;
            if (!videoTrackOffered(offer, q, pick.videoFormat)) pick.videoFormat = 'best';
          });
        row('quality', labels.quality, ['best', ...qualities], {
          look: 'list',
          isOn: (q) => q === pick.quality,
          name: best((q) => videoQualityName(q, qualities)),
          onPick: quality,
          aside: (q) => size(videoPickSize({ ...pick, quality: q, videoFormat: videoTrackOffered(offer, q, pick.videoFormat) ? pick.videoFormat : 'best' }, offer, 'video')),
        });
        // A format reads as its codec over its container, "H.264" over "MP4".
        // Left to the instance, it shows the one the best track has.
        const format = pick.videoFormat === 'best' ? videoBestFormat(offer, pick.quality) : pick.videoFormat;
        row('format', labels.format, formats, {
          look: 'segments',
          isOn: (f) => f === format,
          name: (f) => videoFormatName(f.split(' ').at(-1)),
          onPick: (f) => redraw(() => (pick.videoFormat = f)),
          off: (f) => !videoTrackOffered(offer, pick.quality, f),
          aside: (f) => (f.includes(' ') ? videoFormatName(f.split(' ')[0]) : ''),
        });
        break;
      }
      case 'audio': {
        if (offer.audioTracks.length) {
          const { rates, codecs } = audioMenuRows(offer);
          const rate = (r) =>
            redraw(() => {
              pick.audioRate = r;
              if (!audioTrackOffered(offer, r, pick.audio)) pick.audio = 'best';
            });
          row('quality', labels.quality, ['best', ...rates], {
            look: 'list',
            isOn: (r) => r === pick.audioRate,
            name: best((r) => r),
            onPick: rate,
            aside: (r) => size(videoPickSize({ ...pick, audioRate: r, audio: audioTrackOffered(offer, r, pick.audio) ? pick.audio : 'best' }, offer, 'audio')),
          });
          const codec = pick.audio === 'best' ? audioBestCodec(offer, pick.audioRate) : pick.audio;
          row('format', labels.format, codecs, {
            look: 'segments',
            isOn: (c) => c === codec,
            name: videoFormatName,
            onPick: (c) => redraw(() => (pick.audio = c)),
            off: (c) => !audioTrackOffered(offer, pick.audioRate, c),
          });
        }
        if (offer.audioLangs.length > 1) {
          const name = (code) => {
            const lang = offer.audioLangs.find((l) => l.code === code);
            return lang.dubbed ? `${code} (${labels.dubbed})` : code;
          };
          const codes = offer.audioLangs.map((l) => l.code);
          row('language', labels.language, codes, { look: 'list', isOn: (c) => c === pick.audioLang, name, onPick: (c) => redraw(() => (pick.audioLang = c)) });
        }
        break;
      }
      case 'subtitle': {
        const { manual, auto } = videoSubtitleChoices(offer, pick);
        const onPick = (code) =>
          redraw(() => {
            pick.subLangs = pick.subLangs.includes(code) ? pick.subLangs.filter((c) => c !== code) : [...pick.subLangs, code];
            pick.subAuto = pick.subLangs.some((c) => auto.includes(c) && !manual.includes(c));
          });
        const isOn = (code) => pick.subLangs.includes(code);
        if (manual.length) row('language', labels.language, manual, { look: 'list', isOn, name: (c) => c, onPick, several: true });
        if (auto.length) row('auto', labels.autoCaptions, auto, { look: 'list', isOn, name: (c) => c.replace(/-orig$/, ''), onPick, several: true });
        row('format', labels.format, VIDEO_SUB_FORMATS, { look: 'segments', isOn: (f) => f === pick.subFormat, name: videoFormatName, onPick: (f) => redraw(() => (pick.subFormat = f)) });
        break;
      }
      case 'thumbnail': {
        const th = offer.thumbnail;
        if (!th) break;
        const formats = videoThumbFormats(offer);
        const one = formats.length === 1 ? videoFormatName(formats[0]) : '';
        const size = th.width && th.height ? `${th.width}×${th.height}` : '';
        if (size || one) nodes.push(note([size, one].filter(Boolean).join(' ')));
        // A row of one choice would offer nothing.
        if (!one) row('format', labels.format, formats, { look: 'segments', isOn: (f) => f === pick.thumbFormat, name: videoFormatName, onPick: (f) => redraw(() => (pick.thumbFormat = f)) });
        break;
      }
    }
    return nodes;
  }

  function card(head, body) {
    const part = document.createElement('div');
    part.className = 'card';
    part.append(head);
    if (body.length) {
      const box = document.createElement('div');
      box.className = 'choices';
      box.append(...body);
      part.append(box);
    }
    return part;
  }

  /** Draws the menu for the current pick, with the keyboard back on the
   *  control it was on, or on the first one. */
  function renderMenu(focusKey = root.activeElement?.dataset.key) {
    const redraw = (change) => {
      change();
      renderMenu();
    };
    const offer = offered?.answer;
    const items = [];
    if (page.playlist) items.push(card(toggle(labels.playlist, playlist, 'playlist', () => redraw(() => (playlist = !playlist))), []));
    for (const part of VIDEO_PARTS) {
      if (offer && !videoHas(offer, part)) continue;
      const on = pick.parts.includes(part);
      const head = toggle(labels.parts[part], on, part, () => redraw(() => togglePart(part)));
      items.push(card(head, offer && on ? partChoices(part, offer, redraw) : []));
    }
    menu.setAttribute('aria-label', labels.more);
    menu.setAttribute('aria-busy', String(!offer));
    if (!offer) items.push(note(labels.checking, true));
    else if (!offer.known) items.push(note(labels.unknown));
    const go = control(labels.send, () => void sendVideo(async () => videoExact(pick, offered?.answer ?? videoOffer(null))));
    go.className = 'go';
    go.dataset.key = 'go';
    // The sizes are those of one video, so a whole playlist shows none.
    const total = offer?.known && !playlist ? videoPickTotal(pick, offer) : undefined;
    if (total) {
      const weight = document.createElement('span');
      weight.className = 'total';
      weight.textContent = `· ${videoSize(total, labels.lang)}`;
      go.append(weight);
      go.setAttribute('aria-label', `${labels.send}, ${videoSize(total, labels.lang)}`);
    }
    const foot = document.createElement('div');
    foot.className = 'foot';
    foot.append(go);
    menu.replaceChildren(...items, foot);
    const controls = menu.querySelectorAll('button');
    ([...controls].find((b) => b.dataset.key === focusKey) ?? controls[0]).focus();
  }

  function openMenu() {
    pick = videoDefaultsFor(stored, page.site);
    void askFormats();
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

  // "Done" and a failed download open the instance's downloads, where the
  // file is, or the row with its error and a retry.
  send.addEventListener('click', (e) => {
    if (!e.isTrusted) return;
    if (result?.instance) chrome.runtime.sendMessage({ type: 'knightloader-video-open', instance: result.instance }).catch(() => {});
    else void sendVideo(clickPick);
  });
  more.addEventListener('click', (e) => {
    if (!e.isTrusted) return;
    if (menu.hidden) openMenu();
    else closeMenu();
  });
  // Up and down go from one stop to the next, as Tab does: each switch, the
  // picked choice of each radio group, each subtitle language and the
  // buttons at the foot. Inside a radio group they pick instead.
  menu.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      closeMenu();
      more.focus();
      return;
    }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    e.preventDefault();
    const stops = [...menu.querySelectorAll('button')].filter((b) => !b.disabled && b.getAttribute('tabindex') !== '-1');
    const at = stops.indexOf(root.activeElement);
    stops[(at + (e.key === 'ArrowDown' ? 1 : stops.length - 1)) % stops.length]?.focus();
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

  // The keyboard shortcut (video-worker.js) counts as a click, wherever the
  // button is on the page, shown or waiting for the pointer.
  chrome.runtime.onMessage.addListener((msg) => {
    if (msg?.type === 'knightloader-video-shortcut' && host.isConnected) void sendVideo(clickPick);
  });

  // The popup's answer, from the worker. None means it was closed without a
  // pick, and the button goes back to rest with nothing to report.
  chrome.runtime.onMessage.addListener((msg) => {
    if (msg?.type !== 'knightloader-video-result' || !asking) return;
    asking = false;
    if (msg.answer) {
      showResult(msg.answer);
      return;
    }
    sending = false;
    update();
  });

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

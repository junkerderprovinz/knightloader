// Shared by background.js (importScripts), popup.js, picker.js and options.js
// (<script> tag): the instance card, the language listbox and the other pieces
// more than one page draws, so the pages stay alike.

/**
 * deploymentLabel names what an instance is, since there is no address to
 * show. The keys are written out because check-locales.mjs cannot see a key
 * assembled at runtime.
 */
function deploymentLabel(dep) {
  if (dep === 'desktop') return t('picker.deployment.desktop');
  if (dep === 'container') return t('picker.deployment.container');
  return t('picker.viaRelay');
}

/**
 * shake plays GlimStone's failure feedback on the control that was clicked.
 *
 * Removing and re-adding the class in one frame does not restart an animation,
 * and cloning the node would drop its listeners, so a forced reflow sits in
 * between.
 */
function shake(el) {
  if (!el) return;
  el.classList.remove('glim-shake');
  // Reading a layout property flushes the class removal.
  void el.offsetWidth;
  el.classList.add('glim-shake');
  el.addEventListener('animationend', () => el.classList.remove('glim-shake'), { once: true });
}

/**
 * instanceCard draws one instance as the web UI's Instances tab does: the mark
 * along the left edge, the name, what it is, and a badge for the default.
 *
 * `onPick` makes the card the chooser for this send. `onSetDefault` sits in the
 * right-click menu, since the default is set once and a button on every card
 * would be louder than it deserves. `index` is the card's palette position.
 */
function instanceCard(inst, { index, isDefault, isChosen, onPick, onSetDefault, status, onQueue, onOpen }) {
  // A div, since the card holds buttons and a button inside a button is
  // dropped by browsers. A pickable card gets the radio role and its keyboard
  // handling instead.
  const card = document.createElement('div');
  card.className = 'glim-instance';
  card.dataset.instanceId = inst.instanceId;
  if (onPick) {
    card.setAttribute('role', 'radio');
    card.setAttribute('aria-checked', String(!!isChosen));
    // aria-checked is for screen readers; .glim-active lets the reactive
    // rainbow colour the chosen card at rest, not only on hover.
    card.classList.toggle('glim-active', !!isChosen);
    card.tabIndex = 0;
  }
  if (typeof index === 'number') setHue(card, index);

  const mark = document.createElement('span');
  mark.className = 'glim-instance-mark';
  const logo = document.createElement('img');
  logo.src = 'logo.svg';
  logo.alt = '';
  logo.setAttribute('aria-hidden', 'true');
  mark.appendChild(logo);

  const body = document.createElement('span');
  body.className = 'glim-instance-body';
  const name = document.createElement('span');
  name.className = 'glim-instance-name';
  name.textContent = instanceLabel(inst);
  const what = document.createElement('span');
  what.className = 'glim-instance-what';
  what.textContent = deploymentLabel(inst.deployment);

  // The status badge shares the name's line. `undefined` means the caller did
  // not ask, and a card that never checked must not claim to be online.
  if (status !== undefined) {
    const top = document.createElement('span');
    top.className = 'glim-instance-top';
    const badge = document.createElement('span');
    badge.className = status === null ? 'glim-status glim-status--off' : 'glim-status glim-status--on';
    badge.textContent = t(status === null ? 'instance.offline' : 'instance.online');
    top.append(name, badge);
    body.append(top, what);
  } else {
    body.append(name, what);
  }

  // What the instance is doing. The popup does not ask, so it stays fast;
  // null means it asked and got no answer.
  if (status !== undefined) {
    const line = document.createElement('span');
    line.className = 'glim-instance-stats glim-num';
    line.textContent = status === null ? t('instance.offline') : statusLine(status);
    if (status === null) line.classList.add('glim-instance-stats--off');
    body.appendChild(line);
  }

  card.append(mark, body);

  if (isDefault) {
    const badge = document.createElement('span');
    badge.className = 'glim-instance-badge';
    badge.textContent = t('options.defaultBadge');
    card.appendChild(badge);
  }

  // The square actions, only when the caller supplies handlers, so the popup
  // and the send-to window keep a compact card.
  if (onQueue || onOpen) {
    const actions = document.createElement('span');
    actions.className = 'glim-instance-actions';
    const halted = status?.queue?.halted === true;
    const live = status !== null && status !== undefined;

    if (onQueue) {
      // Two controls rather than one toggle whose glyph flips, which would have
      // to be read before every press.
      actions.appendChild(
        squareAction(GLYPH_PLAY, t('instance.start'), !live || !halted, (el) => onQueue(inst, false, el)),
      );
      actions.appendChild(
        squareAction(GLYPH_STOP, t('instance.stop'), !live || halted, (el) => onQueue(inst, true, el), t('instance.haltedHint')),
      );
    }
    if (onOpen) {
      // Disabled rather than hidden without an address, so every card has the
      // same controls.
      const url = status?.webUrl ?? '';
      actions.appendChild(squareAction(GLYPH_OPEN, t('instance.open'), !url, (el) => onOpen(inst, url, el)));
    }
    card.appendChild(actions);
  }

  if (onPick) {
    card.addEventListener('click', (e) => {
      // Pressing an action must not also change where the next send goes.
      if (e.target.closest('.glim-instance-actions')) return;
      onPick(inst);
    });
    card.addEventListener('keydown', (e) => {
      if (e.key === ' ' || e.key === 'Enter') {
        e.preventDefault();
        onPick(inst);
      }
    });
  }
  if (onSetDefault) {
    card.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      openInstanceMenu(e, inst, isDefault, onSetDefault);
    });
  }
  return card;
}

/* The card's glyphs, built as nodes because Mozilla's linter, a release gate
   here, fails an innerHTML assignment from a variable. */
const GLYPH_PLAY = 'M5 3.5v9l8-4.5z';
const GLYPH_STOP = 'M4.5 4.5h7v7h-7z';
const GLYPH_OPEN = 'M9 2h5v5h-1.5V4.56L7.8 9.26 6.74 8.2l4.7-4.7H9zM3 4h4v1.5H4.5v6h6V9H12v4H3z';

function squareAction(d, label, disabled, onClick, extraTip) {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'glim-square';
  b.disabled = !!disabled;
  const tip = extraTip ? `${label}: ${extraTip}` : label;
  b.setAttribute('aria-label', label);
  b.setAttribute('data-tip', tip);
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 16 16');
  // A glyph alone in a square fills half of it (GlimStone 1.8.0).
  svg.setAttribute('width', '16');
  svg.setAttribute('height', '16');
  svg.setAttribute('aria-hidden', 'true');
  const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  path.setAttribute('fill', 'currentColor');
  path.setAttribute('d', d);
  svg.appendChild(path);
  b.appendChild(svg);
  // The button passes itself so a failure shakes the clicked node, since the
  // card is rebuilt on every render.
  b.addEventListener('click', () => onClick(b));
  return b;
}

/**
 * statusLine turns the two queue readings into one line of standalone status
 * words and counters, which read correctly in every language where a sentence
 * such as "3 running" would not.
 */
function statusLine(s) {
  const q = s.queue ?? {};
  const c = s.counters ?? {};
  const parts = [];
  if (q.halted) parts.push(t('instance.paused'));
  else if ((c.running ?? q.running ?? 0) > 0) parts.push(t('instance.running'));
  else if ((c.files ?? 0) > 0) parts.push(t('instance.queued'));

  // The file count is shown even at zero, so the card keeps its height when a
  // download appears.
  parts.push(`${c.files ?? 0} ${t('instance.files')}`);
  if ((c.remaining ?? 0) > 0) parts.push(`${fmtBytes(c.remaining)} ${t('instance.left')}`);
  if ((c.speed ?? 0) > 0) parts.push(`${fmtBytes(c.speed)}/s`);
  return parts.join(' · ');
}

/** Binary units, as the web UI's fmtBytes. */
function fmtBytes(n) {
  if (!Number.isFinite(n) || n <= 0) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

/**
 * The card's right-click menu. It has one entry, and the entry explains what
 * the default is. A second right-click replaces the menu; a click outside,
 * Escape or a scroll closes it.
 */
function openInstanceMenu(event, inst, isDefault, onSetDefault) {
  closeInstanceMenu();
  const menu = document.createElement('div');
  menu.className = 'glim-menu';
  menu.id = 'glim-instance-menu';

  const item = document.createElement('button');
  item.type = 'button';
  item.textContent = isDefault ? t('options.alreadyDefault') : t('options.makeDefault');
  item.disabled = !!isDefault;
  item.addEventListener('click', async () => {
    closeInstanceMenu();
    await onSetDefault(inst);
  });
  menu.appendChild(item);

  document.body.appendChild(menu);
  // Measured once in the DOM, then clamped into the small popup window.
  const r = menu.getBoundingClientRect();
  // On a right-to-left page the menu hangs to the left of the pointer. left
  // and top stay physical because pointer coordinates are.
  const rtl = document.documentElement.dir === 'rtl';
  const anchorX = rtl ? event.clientX - r.width : event.clientX;
  const x = Math.min(anchorX, document.documentElement.clientWidth - r.width - 8);
  const y = Math.min(event.clientY, document.documentElement.clientHeight - r.height - 8);
  menu.style.left = `${Math.max(8, x)}px`;
  menu.style.top = `${Math.max(8, y)}px`;

  setTimeout(() => {
    // Only presses outside close it; otherwise pressing the entry would remove
    // the menu before the click lands.
    document.addEventListener('pointerdown', onMenuPointerDown, true);
    document.addEventListener('keydown', escInstanceMenu);
    window.addEventListener('scroll', closeInstanceMenu, { once: true, capture: true });
  }, 0);
}

function onMenuPointerDown(e) {
  const menu = document.getElementById('glim-instance-menu');
  if (menu && !menu.contains(e.target)) closeInstanceMenu();
}

function escInstanceMenu(e) {
  if (e.key === 'Escape') closeInstanceMenu();
}

function closeInstanceMenu() {
  document.getElementById('glim-instance-menu')?.remove();
  document.removeEventListener('keydown', escInstanceMenu);
  document.removeEventListener('pointerdown', onMenuPointerDown, true);
}

/**
 * listbox builds the language picker, since a native <option> cannot show a
 * flag. `options` are { value, label, flag }; the flag is a `fi fi-XX` span
 * from the same stylesheet the web UI uses.
 */
function listbox(host, options, current, onPick) {
  host.innerHTML = '';
  const chosen = options.find((o) => o.value === current) ?? options[0];

  const trigger = document.createElement('button');
  trigger.type = 'button';
  trigger.className = 'glim-listbox-trigger';
  trigger.setAttribute('aria-haspopup', 'listbox');
  trigger.setAttribute('aria-expanded', 'false');
  const flag = document.createElement('span');
  flag.className = `fi fi-${chosen.flag}`;
  const label = document.createElement('span');
  label.textContent = chosen.label;
  const caret = document.createElement('span');
  caret.className = 'glim-listbox-caret';
  caret.textContent = '▾';
  trigger.append(flag, label, caret);
  host.appendChild(trigger);

  let menu = null;
  const close = () => {
    menu?.remove();
    menu = null;
    trigger.setAttribute('aria-expanded', 'false');
  };
  const open = () => {
    if (menu) return close();
    menu = document.createElement('div');
    menu.className = 'glim-listbox-menu';
    menu.setAttribute('role', 'listbox');
    for (const o of options) {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'glim-listbox-option';
      b.setAttribute('role', 'option');
      b.setAttribute('aria-selected', String(o.value === current));
      const f = document.createElement('span');
      f.className = `fi fi-${o.flag}`;
      const l = document.createElement('span');
      l.textContent = o.label;
      b.append(f, l);
      b.addEventListener('click', () => {
        close();
        onPick(o.value);
      });
      menu.appendChild(b);
    }
    host.appendChild(menu);
    trigger.setAttribute('aria-expanded', 'true');
    // Opens scrolled to the current choice.
    menu.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' });
    setTimeout(() => {
      document.addEventListener('pointerdown', onAway);
      document.addEventListener('keydown', onEsc);
    }, 0);
  };
  const onAway = (e) => {
    if (!host.contains(e.target)) {
      close();
      document.removeEventListener('pointerdown', onAway);
      document.removeEventListener('keydown', onEsc);
    }
  };
  const onEsc = (e) => {
    if (e.key === 'Escape') {
      close();
      trigger.focus();
      document.removeEventListener('pointerdown', onAway);
      document.removeEventListener('keydown', onEsc);
    }
  };
  trigger.addEventListener('click', open);

  /**
   * The wheel changes the choice without opening the list, as a closed
   * <select> does (GlimStone rule 14). `passive: false` so preventDefault keeps
   * the page from scrolling at the same time. It stops at both ends rather
   * than wrapping.
   */
  trigger.addEventListener(
    'wheel',
    (event) => {
      if (event.deltaY === 0 || options.length < 2) return;
      event.preventDefault();
      const at = options.findIndex((o) => o.value === chosen.value);
      const next = Math.min(options.length - 1, Math.max(0, at + (event.deltaY > 0 ? 1 : -1)));
      if (next === at) return;
      close();
      onPick(options[next].value);
    },
    { passive: false },
  );
}

/**
 * The translation key for the popup's send button, given what is parked: the
 * current tab when nothing is, otherwise a right-clicked link, image or
 * selection, or a caught Click'n'Load batch. A payload without `kind` keeps the
 * page label.
 */
function sendLabelKey(pending) {
  if (!pending) return 'popup.send';
  if (pending.origin === 'cnl') return 'popup.sendLinks';
  switch (pending.payload?.kind) {
    case 'link':
      return 'popup.sendLink';
    case 'image':
      return 'popup.sendImage';
    case 'selection':
      return 'popup.sendSelection';
    default:
      return 'popup.send';
  }
}

/**
 * How long a caught Click'n'Load batch waits before it sends itself, in
 * seconds; 0 means ask. Five matches JDownloader's own extension. The value is
 * clamped on read too, so a hand-edited one cannot park the popup for an hour.
 */
const CNL_COUNTDOWN_DEFAULT = 5;
const CNL_COUNTDOWN_MAX = 60;

async function readCnlCountdown() {
  try {
    const { cnlCountdown } = await chrome.storage.local.get('cnlCountdown');
    const n = Number(cnlCountdown);
    if (!Number.isFinite(n) || n < 0) return CNL_COUNTDOWN_DEFAULT;
    return Math.min(Math.round(n), CNL_COUNTDOWN_MAX);
  } catch {
    return CNL_COUNTDOWN_DEFAULT;
  }
}

async function writeCnlCountdown(seconds) {
  const n = Number(seconds);
  const safe = !Number.isFinite(n) || n < 0 ? CNL_COUNTDOWN_DEFAULT : Math.min(Math.round(n), CNL_COUNTDOWN_MAX);
  await chrome.storage.local.set({ cnlCountdown: safe });
  return safe;
}

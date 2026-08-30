// Shared by background.js (importScripts), popup.js, picker.js and options.js
// (<script> tag) — one copy of anything more than one of them needs.
//
// It used to hold the whole instance model: a stored {name, url} registry, the
// origin baked into config.default.json at download time, a quickadd URL
// builder, and the entryTarget/entryLabel pair that decided which of a peer's
// two possible addresses to open. All of it went with the phrase rework — see
// group.js, which stores one phrase and asks the relay who is in the group.
//
// What is here now is what all three surfaces draw: the instance card, and the
// listbox the language picker is built from. One implementation each, because
// three pages of one product drawing their own version of the same card is how
// three pages become three slightly different products.

/**
 * deploymentLabel names what an instance IS, since in the phrase model there is
 * no address to show instead.
 *
 * Written out rather than built by interpolating the value into the key, which
 * is what it was first: a key assembled at runtime is invisible to
 * check-locales.mjs, which then reported both translations as dead and would
 * have had them deleted by the next person tidying up.
 */
function deploymentLabel(dep) {
  if (dep === 'desktop') return t('picker.deployment.desktop');
  if (dep === 'container') return t('picker.deployment.container');
  return t('picker.viaRelay');
}

/**
 * instanceCard draws one instance the way the web UI's own Instances tab draws
 * it (jdp, 2026-08-28: "Im erweiterungsfenster sollen die Instanzen auch als
 * cards erscheinen. Gleich wie im instanzentab."): the mark flush against the
 * left edge running the card's full height, the name, what it is, and a badge
 * in the corner for the default.
 *
 * `onPick` makes the card the chooser for this send; `onSetDefault` is offered
 * on right-click rather than as a permanent button, because the default is set
 * once and then not thought about again — a control for it on every card would
 * be louder than the thing it does.
 *
 * `hue` is this card's position in the palette. It is set through setHue() so
 * the class and the custom properties can never be handed out separately.
 */
function instanceCard(inst, { index, isDefault, isChosen, onPick, onSetDefault, status, onQueue, onOpen }) {
  // A DIV, not a BUTTON, since the card grew actions of its own: a button
  // inside a button is invalid markup, and browsers resolve it by dropping the
  // inner one - so the play control would simply not have existed. Where the
  // card is pickable it takes the radio role and the keyboard behaviour that
  // role owes, which a plain div would otherwise have quietly lost.
  const card = document.createElement('div');
  card.className = 'glim-instance';
  card.dataset.instanceId = inst.instanceId;
  if (onPick) {
    card.setAttribute('role', 'radio');
    card.setAttribute('aria-checked', String(!!isChosen));
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
  body.append(name, what);

  // What the instance is DOING, when somebody asked for it. Undefined means
  // the caller did not ask (the popup does not, so it stays fast); null means
  // it asked and got nothing back, which is a different fact and says so.
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

  // The three square actions. Only drawn when the caller supplies handlers, so
  // the popup and the send-to window keep the compact card they had.
  if (onQueue || onOpen) {
    const actions = document.createElement('span');
    actions.className = 'glim-instance-actions';
    const halted = status?.queue?.halted === true;
    const live = status !== null && status !== undefined;

    if (onQueue) {
      // Start and stop as two separate controls rather than one that changes
      // meaning: a single toggle whose glyph flips is a control you have to
      // read before you dare press it, and this one sits next to a list where
      // the answer is "which instance was that again".
      actions.appendChild(
        squareAction(GLYPH_PLAY, t('instance.start'), !live || !halted, () => onQueue(inst, false)),
      );
      actions.appendChild(
        squareAction(GLYPH_STOP, t('instance.stop'), !live || halted, () => onQueue(inst, true), t('instance.haltedHint')),
      );
    }
    if (onOpen) {
      // Disabled rather than hidden when the instance has no address to open:
      // a control that appears on one card and not on another reads as a fault
      // in the card, not as a property of the instance.
      const url = status?.webUrl ?? '';
      actions.appendChild(squareAction(GLYPH_OPEN, t('instance.open'), !url, () => onOpen(inst, url)));
    }
    card.appendChild(actions);
  }

  if (onPick) {
    card.addEventListener('click', (e) => {
      // A press on one of the actions is not a pick: without this, starting a
      // download would also silently change where the next send goes.
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

/* The three glyphs the card draws. Filled shapes, built as nodes rather than
   assigned as innerHTML - Mozilla's linter fails a package on an innerHTML
   assignment from a variable, and it is a release gate here. */
const GLYPH_PLAY = 'M5 3.5v9l8-4.5z';
const GLYPH_STOP = 'M4.5 4.5h7v7h-7z';
const GLYPH_OPEN = 'M9 2h5v5h-1.5V4.56L7.8 9.26 6.74 8.2l4.7-4.7H9zM3 4h4v1.5H4.5v6h6V9H12v4H3z';

function squareAction(d, label, disabled, onClick, extraTip) {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'glim-square';
  b.disabled = !!disabled;
  const tip = extraTip ? `${label} — ${extraTip}` : label;
  b.setAttribute('aria-label', label);
  b.setAttribute('data-tip', tip);
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 16 16');
  svg.setAttribute('width', '14');
  svg.setAttribute('height', '14');
  svg.setAttribute('aria-hidden', 'true');
  const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  path.setAttribute('fill', 'currentColor');
  path.setAttribute('d', d);
  svg.appendChild(path);
  b.appendChild(svg);
  b.addEventListener('click', onClick);
  return b;
}

/**
 * statusLine turns the two queue readings into one line.
 *
 * Built from words the web UI already ships in all 42 languages - its own
 * status labels and counter nouns - rather than from new sentences: a status
 * word standing alone is grammatical everywhere, which "3 running" written out
 * as a sentence is not.
 */
function statusLine(s) {
  const q = s.queue ?? {};
  const c = s.counters ?? {};
  const parts = [];
  if (q.halted) parts.push(t('instance.paused'));
  else if ((c.running ?? q.running ?? 0) > 0) parts.push(t('instance.running'));
  else if ((c.files ?? 0) > 0) parts.push(t('instance.queued'));

  // The file count is ALWAYS shown, zero included. An empty queue with an
  // empty line let the card change height the moment a download appeared, and
  // a list that twitches while you look at it is worse than a line that
  // sometimes reads "0 files". It also answers the question the line exists
  // for - "is this thing doing anything" - instead of leaving it blank.
  parts.push(`${c.files ?? 0} ${t('instance.files')}`);
  if ((c.remaining ?? 0) > 0) parts.push(`${fmtBytes(c.remaining)} ${t('instance.left')}`);
  if ((c.speed ?? 0) > 0) parts.push(`${fmtBytes(c.speed)}/s`);
  return parts.join(' · ');
}

/** Binary units, the same ladder the web UI's own fmtBytes walks. */
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
 * The right-click menu. One entry today, and deliberately still a menu: a
 * right-click that opens nothing teaches people the gesture does nothing here,
 * and the entry says out loud what the default even means.
 *
 * Only ever one menu on the page — a second right-click replaces the first
 * rather than stacking, and any click, Escape or scroll closes it.
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
  // Measured after it is in the DOM, then clamped: a card near the bottom of a
  // 360px popup would otherwise open a menu off the end of the window.
  const r = menu.getBoundingClientRect();
  const x = Math.min(event.clientX, document.documentElement.clientWidth - r.width - 8);
  const y = Math.min(event.clientY, document.documentElement.clientHeight - r.height - 8);
  menu.style.left = `${Math.max(8, x)}px`;
  menu.style.top = `${Math.max(8, y)}px`;

  setTimeout(() => {
    // OUTSIDE only, and this is not a nicety: a plain
    // `pointerdown -> close` listener also fires for the press on the menu's
    // own entry, which removes the menu before that press ever becomes a click
    // — so the entry looked dead and the default never changed. Caught by
    // driving it rather than by reading it.
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
 * listbox builds the language picker: a field-shaped trigger that opens a real
 * list, because a native <option> can hold plain text and nothing else and a
 * flag beside a language name is therefore impossible in one.
 *
 * `options` are { value, label, flag }. The flag is a `fi fi-XX` span from the
 * same generated stylesheet the web UI uses, so the two lists show the identical
 * artwork rather than two approximations of it.
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
    // Scrolled to the current choice: a list of 42 languages that opens at the
    // top makes somebody scroll to find where they already are.
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
}

/**
 * How long a caught Click'n'Load batch waits before it sends itself.
 *
 * Seconds; 0 means "ask me", which is what this extension did unconditionally
 * before the setting existed. Five is the default because that is roughly what
 * JDownloader's own extension gives you - long enough to read where it is
 * going and reach for another instance, short enough that nobody waits.
 *
 * Clamped on the way OUT rather than only on the way in: a value written by an
 * older build, or by hand in the storage inspector, must not be able to park a
 * popup for an hour.
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

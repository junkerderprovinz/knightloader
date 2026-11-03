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
  if (dep === 'server') return t('picker.deployment.server');
  return t('picker.viaRelay');
}

/**
 * replay plays a one-shot animation class again, also on an element that still
 * has it. Removing and re-adding the class in one frame does not restart an
 * animation, and cloning the node would drop its listeners, so a forced reflow
 * sits in between. The class goes once its own animation ends; one that ends
 * inside the element, such as a checkmark being drawn, does not count.
 */
function replay(el, cls) {
  if (!el) return;
  el.classList.remove(cls);
  // Reading a layout property flushes the class removal.
  void el.offsetWidth;
  el.classList.add(cls);
  const done = (event) => {
    if (event.target !== el) return;
    el.removeEventListener('animationend', done);
    el.classList.remove(cls);
  };
  el.addEventListener('animationend', done);
}

/**
 * shake plays GlimStone's failure feedback on the control that was clicked,
 * and confirmPulse its success pulse. An action either fails or succeeds, and
 * each takes the other's class away, since two animation rules on one element
 * would let only one of them play.
 */
function shake(el) {
  el?.classList.remove('glim-confirm');
  replay(el, 'glim-shake');
}

function confirmPulse(el) {
  el?.classList.remove('glim-shake');
  replay(el, 'glim-confirm');
}

/** A motion dial's current duration in milliseconds, 0 where the level sets none. */
function motionMs(dial) {
  const value = getComputedStyle(document.documentElement).getPropertyValue(dial).trim();
  if (value.endsWith('ms')) return parseFloat(value) || 0;
  return parseFloat(value) * 1000 || 0;
}

/**
 * staggerRows gives list rows GlimStone's staggered entrance the first time
 * their key is drawn. `rows` are [key, element] pairs. The lists here are drawn
 * afresh on every update, the popup's every two seconds, so `starts` keeps the
 * moment each row's entrance began, and a row drawn again carries on from
 * there, or stands finished once that is past.
 */
function staggerRows(starts, rows) {
  const now = performance.now();
  const step = motionMs('--motion-stagger-step');
  const cap = motionMs('--motion-stagger-cap');
  let arriving = 0;
  for (const [key, row] of rows) {
    if (!starts.has(key)) starts.set(key, now + Math.min(arriving++ * step, cap));
    row.classList.add('glim-stagger-row');
    row.style.animationDelay = `${starts.get(key) - now}ms`;
  }
}

/**
 * instanceCard draws one instance as the web UI's Instances tab does: the mark
 * along the left edge, the name, what it is, and a badge for the default.
 *
 * `onPick` makes the card the chooser for this send. `onSetDefault` sits in the
 * right-click menu, since the default is set once and a button on every card
 * would be louder than it deserves. `index` is the card's palette position.
 */
function instanceCard(inst, { index, isDefault, isChosen, onPick, onSetDefault, status, onOpen }) {
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

  const mark = cardMark();

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
    badge.textContent = t(status === null ? 'instance.notConnected' : 'instance.connected');
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
    line.textContent = status === null ? t('instance.notConnected') : statusLine(status);
    if (status === null) line.classList.add('glim-instance-stats--off');
    if (status && downloading(status)) livePulse(line);
    body.appendChild(line);
  }

  card.append(mark, body);

  // The star marks the default entry wherever GlimStone draws one.
  if (isDefault) {
    const badge = document.createElement('span');
    badge.className = 'glim-instance-badge';
    badge.append(glimGlyph('IconStar', 12, { tight: true }), document.createTextNode(t('options.defaultBadge')));
    card.appendChild(badge);
  }

  // The popup links to each instance's web interface, which is where its
  // queue is run; the extension only hands links over. The options page sets
  // the group up and only lists it.
  if (onOpen) {
    const actions = document.createElement('span');
    actions.className = 'glim-instance-actions';
    // Disabled rather than hidden without an address, so every card has the
    // same control.
    const url = status?.webUrl ?? '';
    actions.appendChild(squareAction('IconExternalLink', t('instance.open'), !url, (el) => onOpen(inst, url, el)));
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

/**
 * deviceCard draws one phone or browser of the group (GET /api/group/apps) as
 * the web UI's AppCard does, without actions, since the settings page does not
 * operate devices. `own` marks this browser.
 */
function deviceCard(app, { index, own }) {
  const card = document.createElement('div');
  card.className = 'glim-instance';
  card.dataset.instanceId = app.id;
  if (typeof index === 'number') setHue(card, index);

  const mark = cardMark();

  const body = document.createElement('span');
  body.className = 'glim-instance-body';
  const top = document.createElement('span');
  top.className = 'glim-instance-top';
  const name = document.createElement('span');
  name.className = 'glim-instance-name';
  name.textContent = app.name?.trim() || app.id.slice(0, 16);
  const badge = document.createElement('span');
  top.append(name, badge);
  // A browser opens a session only while it is used, so one that is approved
  // reads as paired rather than as gone.
  if (app.connected) {
    badge.className = 'glim-status glim-status--on';
    badge.textContent = t('instance.connected');
  } else if (app.approved === false) {
    badge.className = 'glim-status glim-status--wait';
    badge.textContent = t('device.pending');
  } else if (app.deployment === 'extension') {
    badge.className = 'glim-status glim-status--on';
    badge.textContent = t('device.paired');
    top.appendChild(glimInfoIcon(t('device.pairedTip')));
  } else {
    inactiveBadge(badge);
  }

  const what = document.createElement('span');
  what.className = 'glim-instance-what';
  what.textContent = app.deployment === 'extension' ? t('device.kindExtension') : t('device.kindMobile');
  body.append(top, what);
  if (app.lastSeen > 0) body.appendChild(lastSeenLine(app.lastSeen));

  card.append(mark, body);
  if (own) {
    const self = document.createElement('span');
    self.className = 'glim-instance-badge';
    self.textContent = t('device.thisDevice');
    card.appendChild(self);
  }
  return card;
}

/**
 * awayCard draws an instance approved in the group that is not there now, such
 * as a desktop app that was closed, as the web UI's AwayCard does: what it last
 * said it is and when it was last seen. It takes no send until it is back.
 */
function awayCard(member, { index }) {
  const card = document.createElement('div');
  card.className = 'glim-instance';
  card.dataset.instanceId = member.id;
  if (typeof index === 'number') setHue(card, index);

  const body = document.createElement('span');
  body.className = 'glim-instance-body';
  const top = document.createElement('span');
  top.className = 'glim-instance-top';
  const name = document.createElement('span');
  name.className = 'glim-instance-name';
  name.textContent = instanceLabel({ name: member.name, instanceId: member.id });
  const badge = document.createElement('span');
  inactiveBadge(badge);
  top.append(name, badge);
  const what = document.createElement('span');
  what.className = 'glim-instance-what';
  what.textContent = deploymentLabel(member.deployment);
  body.append(top, what);
  if (member.lastSeen > 0) body.appendChild(lastSeenLine(member.lastSeen));

  card.append(cardMark(), body);
  return card;
}

/** The logo along a card's start edge. */
function cardMark() {
  const mark = document.createElement('span');
  mark.className = 'glim-instance-mark';
  const logo = document.createElement('img');
  logo.src = 'logo.svg';
  logo.alt = '';
  logo.setAttribute('aria-hidden', 'true');
  mark.appendChild(logo);
  return mark;
}

/** Grey rather than red, since being closed or away is no fault. */
function inactiveBadge(badge) {
  badge.className = 'glim-status glim-status--idle';
  badge.textContent = t('instance.inactive');
}

function lastSeenLine(unix) {
  const seen = document.createElement('span');
  seen.className = 'glim-instance-stats';
  const when = new Date(unix * 1000).toLocaleString(currentLanguage(), { dateStyle: 'medium', timeStyle: 'short' });
  seen.textContent = t('device.lastSeen', { time: when });
  return seen;
}

/** A square glyph button on a card, named by its tooltip and aria-label. */
function squareAction(glyphName, label, disabled, onClick) {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'glim-square glim-btn-neutral';
  b.disabled = !!disabled;
  b.setAttribute('aria-label', label);
  b.setAttribute('data-tip', label);
  // A glyph alone in a square fills half of it.
  b.appendChild(glimGlyph(glyphName, 21));
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
  else if (downloading(s)) parts.push(t('instance.running'));
  else if ((c.files ?? 0) > 0) parts.push(t('instance.queued'));

  // The file count is shown even at zero, so the card keeps its height when a
  // download appears.
  parts.push(`${c.files ?? 0} ${tn('instance.files', c.files ?? 0)}`);
  if ((c.remaining ?? 0) > 0) parts.push(`${fmtBytes(c.remaining)} ${t('instance.left')}`);
  if ((c.speed ?? 0) > 0) parts.push(`${fmtBytes(c.speed)}/s`);
  return parts.join(' · ');
}

/** Whether an instance is downloading, the status line's "running". */
function downloading(s) {
  return !s.queue?.halted && (s.counters?.running ?? s.queue?.running ?? 0) > 0;
}

/**
 * livePulse marks a line as in progress with GlimStone's `.glim-live` pulse.
 * The popup draws its cards afresh every two seconds, so the pulse starts at
 * the phase one clock gives every line rather than at full strength each time.
 */
function livePulse(el) {
  el.classList.add('glim-live');
  const period = motionMs('--motion-pulse-dur');
  if (period > 0) el.style.animationDelay = `-${Math.round(performance.now() % period)}ms`;
}

/** Decimal units, as the web UI's fmtBytes. */
function fmtBytes(n) {
  if (!Number.isFinite(n) || n <= 0) return '0 B';
  const units = ['B', 'kB', 'MB', 'GB', 'TB'];
  let i = 0;
  let v = n;
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000;
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
 * listbox builds the extension's one dropdown, the web UI's Dropdown and
 * GlimStone's Select in plain DOM: a field-shaped trigger that opens a list
 * with a check mark on the value in force. Never a native <select>, whose open
 * list is the operating system's widget that no theme here reaches.
 *
 * `options` are { value, label, flag }, the flag a `fi fi-XX` span from the
 * stylesheet the web UI uses. `name` is what the dropdown chooses, the
 * accessible name with the value after it. `width` is `fill`, which takes the
 * line, or `steady`, as wide as the longest option, so choosing never moves
 * what stands beside it.
 *
 * The arrow keys, Enter and Space open the list; inside it the arrows, Home
 * and End move, a letter jumps to the next label starting with it, Enter
 * picks, and Escape or Tab closes. The wheel steps the value while the trigger
 * has focus.
 */
function listbox(host, options, current, onPick, { name = '', width = 'fill' } = {}) {
  const hadFocus = host.contains(document.activeElement);
  host.replaceChildren();
  host.classList.add('glim-listbox');
  host.classList.toggle('glim-listbox--steady', width === 'steady');
  const chosen = options.find((o) => o.value === current) ?? options[0];
  const withFlags = options.some((o) => o.flag);

  const trigger = document.createElement('button');
  trigger.type = 'button';
  trigger.className = 'glim-listbox-trigger';
  trigger.setAttribute('aria-haspopup', 'listbox');
  trigger.setAttribute('aria-expanded', 'false');
  if (name) trigger.setAttribute('aria-label', `${name}: ${chosen.label}`);
  if (withFlags) {
    const flag = document.createElement('span');
    flag.className = `fi fi-${chosen.flag}`;
    trigger.appendChild(flag);
  }
  // Every label in one grid cell with only the chosen one visible, so a steady
  // trigger is as wide as its longest option.
  const value = document.createElement('span');
  value.className = 'glim-listbox-value';
  for (const label of width === 'steady' ? [...new Set([chosen.label, ...options.map((o) => o.label)])] : [chosen.label]) {
    const span = document.createElement('span');
    span.textContent = label;
    if (label !== chosen.label) {
      span.className = 'glim-listbox-ghost';
      span.setAttribute('aria-hidden', 'true');
    }
    value.appendChild(span);
  }
  const caret = document.createElement('span');
  caret.className = 'glim-listbox-caret';
  caret.appendChild(glimGlyph('IconExpand', 14));
  trigger.append(value, caret);
  host.appendChild(trigger);

  let menu = null;
  const items = () => [...(menu?.querySelectorAll('.glim-listbox-option') ?? [])];
  const close = (refocus) => {
    if (!menu) return;
    menu.remove();
    menu = null;
    trigger.setAttribute('aria-expanded', 'false');
    document.removeEventListener('pointerdown', onAway);
    if (refocus) trigger.focus();
  };
  const pick = (o) => {
    close(true);
    if (o.value !== chosen.value) onPick(o.value);
  };
  const onAway = (e) => {
    if (!host.contains(e.target)) close(false);
  };
  const open = () => {
    menu = document.createElement('div');
    menu.className = 'glim-listbox-menu';
    menu.setAttribute('role', 'listbox');
    if (name) menu.setAttribute('aria-label', name);
    for (const o of options) {
      const b = document.createElement('button');
      b.type = 'button';
      b.tabIndex = -1;
      b.className = 'glim-listbox-option';
      b.setAttribute('role', 'option');
      b.setAttribute('aria-selected', String(o.value === chosen.value));
      if (withFlags) {
        const f = document.createElement('span');
        f.className = `fi fi-${o.flag}`;
        b.appendChild(f);
      }
      const l = document.createElement('span');
      l.className = 'glim-listbox-label';
      l.textContent = o.label;
      const check = document.createElement('span');
      check.className = 'glim-listbox-check';
      check.appendChild(glimGlyph('IconCheck', 14));
      b.append(l, check);
      b.addEventListener('click', () => pick(o));
      menu.appendChild(b);
    }
    menu.addEventListener('keydown', onMenuKey);
    host.appendChild(menu);
    trigger.setAttribute('aria-expanded', 'true');
    // The list starts where the trigger's text does and ends where the
    // trigger ends when the window has no room for it the other way, and it
    // opens above where there is no room below.
    const box = trigger.getBoundingClientRect();
    const list = menu.getBoundingClientRect();
    const view = document.documentElement;
    menu.classList.toggle('glim-listbox-menu--end', list.right > view.clientWidth - 8 || list.left < 8);
    const below = view.clientHeight - box.bottom;
    menu.classList.toggle('glim-listbox-menu--above', below < list.height + 8 && box.top > below);
    const at = items().find((b) => b.getAttribute('aria-selected') === 'true') ?? items()[0];
    at?.focus({ preventScroll: true });
    at?.scrollIntoView({ block: 'nearest' });
    setTimeout(() => document.addEventListener('pointerdown', onAway), 0);
  };
  const onMenuKey = (e) => {
    const all = items();
    const i = all.indexOf(document.activeElement);
    const move = (n) => {
      e.preventDefault();
      all[Math.max(0, Math.min(all.length - 1, n))]?.focus();
    };
    if (e.key === 'ArrowDown') move(i + 1);
    else if (e.key === 'ArrowUp') move(i - 1);
    else if (e.key === 'Home') move(0);
    else if (e.key === 'End') move(all.length - 1);
    else if (e.key === 'Escape') {
      e.preventDefault();
      close(true);
    } else if (e.key === 'Tab') {
      // Back on the trigger first, so the Tab goes on from there.
      close(true);
    }
    else if (e.key.length === 1 && /\S/.test(e.key)) {
      // The next label after the focused one that starts with the letter.
      const key = e.key.toLocaleLowerCase();
      for (let step = 1; step <= all.length; step++) {
        const b = all[(i + step) % all.length];
        if (b.textContent.trim().toLocaleLowerCase().startsWith(key)) {
          b.focus();
          break;
        }
      }
    }
  };
  trigger.addEventListener('click', () => (menu ? close(true) : open()));
  trigger.addEventListener('keydown', (e) => {
    if (menu || !['ArrowDown', 'ArrowUp'].includes(e.key)) return;
    e.preventDefault();
    open();
  });

  /**
   * The wheel changes the choice without opening the list (GlimStone rule 14),
   * but only while the trigger has focus, as the number fields do: on hover
   * alone, scrolling the page past it would switch the value. `passive: false`
   * so preventDefault keeps the page from scrolling at the same time. It stops
   * at both ends rather than wrapping.
   */
  trigger.addEventListener(
    'wheel',
    (event) => {
      if (document.activeElement !== trigger || event.deltaY === 0 || options.length < 2) return;
      event.preventDefault();
      const at = options.findIndex((o) => o.value === chosen.value);
      const next = Math.min(options.length - 1, Math.max(0, at + (event.deltaY > 0 ? 1 : -1)));
      if (next === at) return;
      close(false);
      onPick(options[next].value);
    },
    { passive: false },
  );
  if (hadFocus) trigger.focus();
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
 * joinCnlBatches is a parked Click'n'Load batch with a later one added. Some
 * sites post their links in more than one submission, the first part alone and
 * the rest after it, and the later post must not replace the earlier one.
 */
function joinCnlBatches(held, next) {
  const lines = [held.text, next.text].flatMap((t) => String(t ?? '').split('\n')).filter(Boolean);
  return { ...held, text: [...new Set(lines)].join('\n'), title: held.title || next.title };
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

/**
 * The site access Click'n'Load runs on. A button can sit on any site, and the
 * jdcheck.js redirect needs access to the page that asks as well as to the
 * port. It is optional, so the browser asks when the switch is turned on.
 */
const CNL_ACCESS = { origins: ['<all_urls>'] };

/**
 * Click'n'Load's state. `wanted` is the stored switch, where nothing stored
 * means on; `on` also needs the access, which the browser can withhold or take
 * back at any time. Wanted without access is the state the options page offers
 * to fix with one click, and the popup points there.
 */
async function cnlState() {
  const { cnlEnabled } = await chrome.storage.local.get('cnlEnabled');
  const wanted = cnlEnabled !== false;
  return { wanted, on: wanted && (await chrome.permissions.contains(CNL_ACCESS)) };
}

/**
 * Asks for the access and stores the answer as the switch, so a refusal leaves
 * Click'n'Load off rather than waiting. The request has to come before any
 * other await, while the click still counts as a user gesture; Firefox refuses
 * it otherwise.
 */
async function requestCnlAccess() {
  // It rejects instead of answering false when the gesture is gone or another
  // request is still open.
  const granted = await chrome.permissions.request(CNL_ACCESS).catch(() => false);
  await chrome.storage.local.set({ cnlEnabled: granted });
  return granted;
}

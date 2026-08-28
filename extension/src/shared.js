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
function instanceCard(inst, { index, isDefault, isChosen, onPick, onSetDefault }) {
  const card = document.createElement('button');
  card.type = 'button';
  card.className = 'glim-instance';
  card.dataset.instanceId = inst.instanceId;
  if (onPick) {
    card.setAttribute('role', 'radio');
    card.setAttribute('aria-checked', String(!!isChosen));
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

  card.append(mark, body);

  if (isDefault) {
    const badge = document.createElement('span');
    badge.className = 'glim-instance-badge';
    badge.textContent = t('options.defaultBadge');
    card.appendChild(badge);
  }

  if (onPick) card.addEventListener('click', () => onPick(inst));
  if (onSetDefault) {
    card.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      openInstanceMenu(e, inst, isDefault, onSetDefault);
    });
  }
  return card;
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

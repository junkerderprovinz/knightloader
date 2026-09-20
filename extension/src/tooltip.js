/**
 * The tooltip and info bubble engine, ported line for line from
 * glimstone/reference/tooltip.ts.
 *
 * One floating bubble, a direct child of <body>, serves every tooltip and "(i)"
 * icon, so no `overflow: hidden` on a card can clip it. It is clamped into the
 * viewport, flips above when opening below would clip, and its arrow follows
 * the trigger's centre.
 */
(() => {
  const BUBBLE_ID = 'glim-bubble';
  let currentTrigger = null;
  let wired = false;
  // Whether the last input was a pointer rather than a key. Focus that follows
  // a press is a side effect (the click itself, or a dialog handing focus back
  // to its opener), so it must not open the bubble.
  let pointerWasLast = false;

  function bubbleEl() {
    let el = document.getElementById(BUBBLE_ID);
    if (!el) {
      el = document.createElement('div');
      el.id = BUBBLE_ID;
      el.className = 'glim-bubble';
      el.style.display = 'none';
      document.body.appendChild(el);
    }
    return el;
  }

  function hide() {
    const el = document.getElementById(BUBBLE_ID);
    if (el) el.style.display = 'none';
    currentTrigger = null;
  }

  function show(trigger) {
    const tip = trigger.getAttribute('data-tip');
    if (!tip) return;
    const el = bubbleEl();
    const rect = trigger.getBoundingClientRect();
    el.textContent = tip;
    // Shown before measuring: offsetWidth/Height only resolve while the
    // element is actually laid out.
    el.style.display = 'block';
    const vw = document.documentElement.clientWidth || window.innerWidth;
    const vh = document.documentElement.clientHeight || window.innerHeight;
    const w = el.offsetWidth;
    const h = el.offsetHeight;
    const cx = rect.left + rect.width / 2;
    // Clamped so an icon near either edge does not push the bubble off-screen.
    const x = Math.max(8 + w / 2, Math.min(vw - 8 - w / 2, cx));
    el.style.left = `${x}px`;
    // Flips above only when below would clip and there is room above.
    const above = rect.bottom + 8 + h > vh && rect.top - 8 - h >= 0;
    el.classList.toggle('glim-bubble--above', above);
    el.style.top = `${above ? rect.top - 8 - h : rect.bottom + 8}px`;
    el.style.setProperty('--glim-tip-ax', `${Math.max(10, Math.min(w - 10, cx - (x - w / 2)))}px`);
  }

  /**
   * Wires the delegated listeners once for the whole document. Anything with
   * `data-tip` shows the bubble on hover or focus; a stray native `title` is
   * upgraded to `data-tip` on its first hover and the native attribute removed,
   * so the browser's own balloon never fires alongside it.
   */
  function wireTooltips() {
    if (wired) return;
    wired = true;

    function over(event) {
      if (event.type === 'focusin' && pointerWasLast) return;
      const target = event.target;
      if (!(target instanceof Element)) return;
      const trigger = target.closest('[data-tip], [title]');
      if (!trigger) return;
      if (!trigger.getAttribute('data-tip')) {
        const native = trigger.getAttribute('title');
        if (native && native.trim()) {
          trigger.setAttribute('data-tip', native);
          trigger.removeAttribute('title');
        } else {
          return;
        }
      }
      if (trigger === currentTrigger) return;
      currentTrigger = trigger;
      show(trigger);
    }

    function out(event) {
      if (!currentTrigger) return;
      const to = event.relatedTarget;
      if (to instanceof Node && currentTrigger.contains(to)) return;
      hide();
    }

    document.addEventListener('mouseover', over);
    document.addEventListener('mouseout', out);
    document.addEventListener('focusin', over);
    document.addEventListener('focusout', out);
    // A press means the person is acting, not reading.
    document.addEventListener(
      'pointerdown',
      () => {
        pointerWasLast = true;
        hide();
      },
      true,
    );
    document.addEventListener('keydown', () => (pointerWasLast = false), true);
    // Capture, so scrolling an inner container also hides the fixed bubble.
    window.addEventListener('scroll', hide, true);
    // Escape hides the tip but keeps focus on the trigger.
    document.addEventListener('keydown', (event) => {
      if (event.key === 'Escape' && currentTrigger) hide();
    });
  }

  /**
   * The "(i)" trigger. `text` is both the bubble's content and the icon's
   * accessible name, set together so a language switch changes both.
   */
  function infoIcon(text) {
    const span = document.createElement('span');
    span.className = 'glim-info-icon';
    span.innerHTML =
      '<svg viewBox="0 0 16 16" fill="none" aria-hidden="true">' +
      '<circle cx="8" cy="8" r="7" stroke="currentColor" stroke-width="1.3"/>' +
      '<circle cx="8" cy="4.6" r="0.9" fill="currentColor"/>' +
      '<path d="M8 7v4.4" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/></svg>';
    span.setAttribute('data-tip', text);
    span.setAttribute('aria-label', text);
    span.tabIndex = 0;
    return span;
  }

  /**
   * setInfo puts or updates the info icon on a card heading, where every
   * explanation on the page lives. applyStaticText() calls it again on each
   * language change, so it reuses an existing icon.
   */
  function setInfo(headingId, text) {
    const heading = document.getElementById(headingId);
    if (!heading) return;
    const existing = heading.querySelector('.glim-info-icon');
    if (existing) {
      existing.setAttribute('data-tip', text);
      existing.setAttribute('aria-label', text);
      return;
    }
    heading.appendChild(infoIcon(text));
  }

  /**
   * refreshTip re-reads a trigger's `data-tip` while its bubble is open, for a
   * control whose tip changes when pressed, such as the reveal eye. With the
   * keyboard the bubble is already open when Enter runs the click handler.
   */
  function refreshTip(el) {
    if (currentTrigger === el) show(el);
  }

  window.wireTooltips = wireTooltips;
  window.glimInfoIcon = infoIcon;
  window.glimSetInfo = setInfo;
  window.glimRefreshTip = refreshTip;
})();

/**
 * The tooltip / info-bubble engine, ported from
 * glimstone/reference/tooltip.ts.
 *
 * ONE floating bubble, a single <body> child, shared by every hover tooltip and
 * every "(i)" icon on the page — not a popup per trigger. Anchored locally it
 * would be at the mercy of every `overflow: hidden` above it, and one card's
 * clipping would turn an explanation into a sliver.
 *
 * Ported rather than approximated, and that distinction is the point: an
 * adopting app that built its own bubble alongside a different tooltip
 * implementation is the exact inconsistency the design language calls out by
 * name. The positioning maths below is the reference file's, line for line —
 * clamp into the viewport, flip above when opening below would clip, arrow
 * tracks the trigger's real centre even when the bubble has been clamped
 * off-centre.
 *
 * A plain script for a <script> tag, like every other file here: this
 * extension has no build step, so `src/` can be loaded unpacked straight from
 * a checkout.
 */
(() => {
  const BUBBLE_ID = 'glim-bubble';
  let currentTrigger = null;
  let wired = false;

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
    // Clamped into the viewport, not merely centred on the trigger — an icon
    // near either edge would otherwise push the bubble half off-screen.
    const x = Math.max(8 + w / 2, Math.min(vw - 8 - w / 2, cx));
    el.style.left = `${x}px`;
    // Flips above only when opening below would clip AND there is room up
    // there; a trigger at the very top keeps opening downward.
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
    document.addEventListener('pointerdown', hide, true);
    // Capture, so an inner scrollable container counts too — any scroll
    // de-anchors a fixed-position bubble from what it was pointing at.
    window.addEventListener('scroll', hide, true);
    // Escape dismisses it WITHOUT moving focus off the trigger, so a keyboard
    // user can clear a tip covering nearby content and keep working.
    document.addEventListener('keydown', (event) => {
      if (event.key === 'Escape' && currentTrigger) hide();
    });
  }

  /**
   * The "(i)" trigger. Rides the same bubble as everything else. `text` is both
   * the bubble's content and the icon's accessible name — set them together, so
   * a language switch can never change one and leave the other.
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
   * setInfo puts (or updates) an info icon on a card heading.
   *
   * Every explanatory sentence on this page goes through here rather than into
   * a paragraph under the control (jdp, 2026-08-28: "Alle infotexte in i
   * infobubbles!"). Idempotent, because applyStaticText() re-runs on every
   * language change and must not leave a row of icons behind.
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
   * refreshTip re-reads a trigger's `data-tip` while its bubble is open.
   *
   * Needed by any control whose tip changes as a RESULT of clicking it - a
   * reveal eye reading "show the phrase" one moment and "hide it" the next.
   * The events fire in the order pointerdown, focusin, click: the press hides
   * the bubble, focus immediately re-shows it, and only then does the click
   * handler change the text - so without this the bubble sits there stating
   * the opposite of what the button now does. Caught by looking at a
   * screenshot, not by reading the code.
   */
  function refreshTip(el) {
    if (currentTrigger === el) show(el);
  }

  window.wireTooltips = wireTooltips;
  window.glimInfoIcon = infoIcon;
  window.glimSetInfo = setInfo;
  window.glimRefreshTip = refreshTip;
})();

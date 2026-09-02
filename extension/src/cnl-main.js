/**
 * Catches a Click'n'Load submission inside the page, before it leaves.
 *
 * This runs in the MAIN world — the page's own JavaScript context, not the
 * extension's isolated one — and that is the entire reason it exists as its own
 * file. A CnL button posts to http://127.0.0.1:9666 using the page's own fetch,
 * XHR or form, and only code sharing that context can see those calls at all.
 *
 * It is also why this file may not touch a single chrome.* API: the MAIN world
 * has none. Everything it catches is handed over the wall with postMessage, and
 * cnl-relay.js — the same script, in the isolated world — picks it up. That is
 * the standard shape for this, and the same one JDownloader's own MV3 extension
 * uses (cnlInterceptorMain.js beside cnlInterceptor.js).
 *
 * document_start, always: the site's CnL code can run before DOMContentLoaded,
 * and a patch installed after it has already captured `fetch` is a patch that
 * never fires.
 *
 * What the page gets back is what a real JDownloader answers — "success\r\n",
 * matching internal/cnl/cnl.go — so the button reports what it always reports
 * and the site has no way to tell the difference. A submission that silently
 * appeared to fail would be worse than no interception at all: the user would
 * click again.
 */
(() => {
  const HOSTS = ['127.0.0.1:9666', 'localhost:9666'];
  const TAG = 'knightloader-cnl';

  // --- detection ---------------------------------------------------------
  // Before a site renders its Click'n'Load button it loads
  // <script src="http://127.0.0.1:9666/jdcheck.js"> and checks whether the
  // global came out true. With nothing on that port the script fails and the
  // button never appears, so there would be no submission to catch.
  //
  // BOTH halves are needed, and finding that out took a live container.
  //
  // This half declares the globals in the page at document_start, before any
  // script the page brings, and it costs no permission at all. It answers every
  // site that simply READS `jdownloader`.
  //
  // It does not answer a site that hangs its decision on the script element's
  // own onload/onerror, because with nothing listening on that port the request
  // fails and `onerror` fires whatever the globals say. filecrypt is such a
  // site: watched live on 2026-08-31, it opened helper.html, asked for
  // jdcheck.js three times, got a network error each time and stopped. The
  // globals were already set. It never looked at them.
  //
  // So the request itself is answered too, by a declarativeNetRequest rule
  // pointing at our own jdcheck.js (see cnl-rules.json). That redirect was
  // built once before and thrown away, for a reason that no longer holds: a DNR
  // redirect needs host permission for the INITIATOR - the website - not merely
  // for the address being requested, and back then this extension asked only
  // for 127.0.0.1:9666. It declares <all_urls> now, for the interception
  // itself, so the rule costs nothing extra.
  try {
    if (typeof window.jdownloader === 'undefined') {
      window.jdownloader = true;
      // Some pages read the version too. The same number internal/cnl/cnl.go
      // serves, so both deployments answer a probe identically.
      if (typeof window.version === 'undefined') window.version = '90000';
    }
  } catch {
    // A frame that will not take a property. Nothing else here depends on it.
  }

  const aimedAtCnl = (raw) => {
    try {
      return HOSTS.includes(new URL(raw, location.href).host);
    } catch {
      return false;
    }
  };

  /** Hands one submission to the isolated world. Fire and forget: the page must
   *  not be made to wait on an instance it cannot see. */
  const hand = (path, fields) => {
    // "*", unconditionally, and this is the corrected version of a guard that
    // was too clever. Posting to location.origin drops the message wherever
    // that string is not what the receiving window actually matches: a file://
    // page reports the origin as "file://" while the window's own origin is
    // opaque, a sandboxed iframe and a data: URL report "null". The submission
    // then vanished between the two halves while the page was still told
    // "success" - exactly the silent failure the answer above exists to
    // prevent, arriving through the one door that comment never looked at.
    //
    // Found by driving a real button on a local test page and watching nothing
    // happen. The first fix special-cased "null" and missed "file://", which is
    // the argument against special cases here at all: the set of origins a
    // window does not match itself is not a list worth maintaining.
    //
    // "*" costs nothing that matters. window.postMessage on the window itself
    // delivers to that window only, never to child frames; the receiver insists
    // on event.source === window; and the payload IS the page's own submission,
    // so there is nothing in it the page did not just write itself.
    try {
      window.postMessage({ [TAG]: true, path, fields }, '*');
    } catch {
      // Nothing left to try, and nothing worth breaking the page over.
    }
  };

  const fieldsFromBody = (body) => {
    const out = {};
    if (!body) return out;
    if (typeof body === 'string') {
      for (const [k, v] of new URLSearchParams(body)) out[k] = v;
      return out;
    }
    if (body instanceof URLSearchParams || body instanceof FormData) {
      for (const [k, v] of body) if (typeof v === 'string') out[k] = v;
    }
    return out;
  };

  // The reply a real listener gives. Kept byte-identical to the Go server's.
  const ok = () => new Response('success\r\n', { status: 200, headers: { 'Content-Type': 'text/plain' } });

  // --- fetch -------------------------------------------------------------
  const realFetch = window.fetch;
  if (typeof realFetch === 'function') {
    window.fetch = function (input, init) {
      try {
        const url = typeof input === 'string' ? input : input?.url;
        if (url && aimedAtCnl(url)) {
          const path = new URL(url, location.href).pathname;
          hand(path, fieldsFromBody(init?.body));
          return Promise.resolve(ok());
        }
      } catch {
        // Never let a bug in here take the page's own fetch down with it.
      }
      return realFetch.apply(this, arguments);
    };
  }

  // --- XMLHttpRequest ----------------------------------------------------
  const realOpen = XMLHttpRequest.prototype.open;
  const realSend = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.open = function (method, url) {
    try {
      if (url && aimedAtCnl(url)) this.__klCnl = new URL(url, location.href).pathname;
    } catch {
      /* see above */
    }
    return realOpen.apply(this, arguments);
  };
  XMLHttpRequest.prototype.send = function (body) {
    if (this.__klCnl) {
      hand(this.__klCnl, fieldsFromBody(body));
      // Fake the completed request the page is waiting for. Without this the
      // site sits on a request that never resolves and eventually reports a
      // failure for something that actually worked.
      Object.defineProperty(this, 'readyState', { value: 4, configurable: true });
      Object.defineProperty(this, 'status', { value: 200, configurable: true });
      Object.defineProperty(this, 'responseText', { value: 'success\r\n', configurable: true });
      setTimeout(() => {
        try {
          this.onreadystatechange?.();
          this.onload?.();
          this.dispatchEvent(new Event('readystatechange'));
          this.dispatchEvent(new Event('load'));
        } catch {
          /* the page's own handler threw; not ours to fix */
        }
      }, 0);
      return;
    }
    return realSend.apply(this, arguments);
  };

  // --- forms -------------------------------------------------------------
  // Two paths, and both are needed. A script calling form.submit() never fires
  // a submit event, and a real click on a submit button never calls
  // form.submit() — catching one and not the other misses half the sites.
  const fieldsFromForm = (form) => {
    const out = {};
    try {
      for (const [k, v] of new FormData(form)) if (typeof v === 'string') out[k] = v;
    } catch {
      /* a form mid-teardown */
    }
    return out;
  };

  const realSubmit = HTMLFormElement.prototype.submit;
  HTMLFormElement.prototype.submit = function () {
    try {
      if (this.action && aimedAtCnl(this.action)) {
        hand(new URL(this.action, location.href).pathname, fieldsFromForm(this));
        return;
      }
    } catch {
      /* fall through to the real submit */
    }
    return realSubmit.apply(this, arguments);
  };

  // --- the four quieter ways a container reaches port 9666 ---------------
  //
  // fetch, XHR and forms are the paths a modern CnL button takes, and they
  // were all this file knew. They are not all there are, and a site that uses
  // one of the others is a site where the button reports success and nothing
  // arrives (jdp, 2026-08-30, about a filecrypt container: "Da ploppt das
  // fenster nicht auf").
  //
  // Added blind, and that is stated rather than hidden: the container in
  // question sits behind a "confirm you are not a robot" gate, which is an
  // access control on somebody else's site and not something to automate past.
  // So the button itself was never reached from here. What CAN be done without
  // guessing is to stop leaving whole mechanisms unpatched - each of these is
  // a documented way CnL has been shipped, and each costs one wrapper.
  //
  // No MutationObserver for the element cases: watching the whole document for
  // added nodes is a per-page cost on every page, and patching the property
  // setter catches the assignment itself, which is both cheaper and earlier.

  // window.open: the oldest shape of all, and the one whose failure looks
  // exactly like jdp's report - a window that does not appear.
  const realOpen2 = window.open;
  if (typeof realOpen2 === 'function') {
    window.open = function (url, ...rest) {
      try {
        if (url && aimedAtCnl(url)) {
          const u = new URL(url, location.href);
          hand(u.pathname, Object.fromEntries(u.searchParams));
          // A window object is what the caller expects back. Returning null
          // makes a site think the popup was blocked, which some of them
          // answer with a "please allow popups" banner over a submission that
          // actually worked.
          return window;
        }
      } catch {
        /* fall through to the real open */
      }
      const opened = realOpen2.apply(this, arguments);
      // The window is patched on the way out, not only when its URL is a CnL
      // address: a helper window submits from its own realm, and that realm is
      // created here or nowhere. Also on load, because a window opened blank
      // and then navigated gets a fresh set of prototypes with it.
      try {
        installInOpened(opened);
        opened?.addEventListener?.('load', () => installInOpened(opened), { once: false });
      } catch {
        /* cross-origin; nothing reachable to patch */
      }
      return opened;
    };
  }

  /**
   * Patch a window the page just opened.
   *
   * This is the gap that most likely explains jdp's report (2026-09-02: "wenn
   * ich bei filecrypt auf CnL klicke kommt ein neues JD Browserfenster und das
   * Erweiterungs-popupfenster geht nicht auf").
   *
   * The window.open wrapper above only steps in when the OPENED URL is itself a
   * Click'n'Load address. A site that opens a helper window on its own domain,
   * or an about:blank one, and submits from INSIDE it, goes through that
   * window's own untouched `fetch`, `XMLHttpRequest` and `HTMLFormElement` - a
   * different realm, with a different set of prototypes, that this file never
   * ran in. The request then leaves the browser for real, and a JDownloader
   * listening on the port answers it and raises its own window: exactly what he
   * describes, and from the extension's side completely silent.
   *
   * Only the three paths a helper window realistically uses. The element-src
   * shapes are for a page building markup, which a submission window does not
   * do, and duplicating every wrapper here would double a file whose whole job
   * is to be surgical.
   *
   * Everything is wrapped: reaching into another window throws the moment it is
   * cross-origin, and a throw here would take the page's own window.open with
   * it. `hand` deliberately posts to OUR window, which is where the relay in the
   * isolated world is listening; the opened window has no relay of its own.
   */
  const installInOpened = (win) => {
    if (!win || win === window) return;
    try {
      // Stamped, because 'load' can fire more than once for one window and a
      // second patch would wrap our own wrapper.
      if (win.__klCnlPatched) return;
      win.__klCnlPatched = true;

      const theirFetch = win.fetch;
      if (typeof theirFetch === 'function') {
        win.fetch = function (input, init) {
          try {
            const url = typeof input === 'string' ? input : input?.url;
            if (url && aimedAtCnl(url)) {
              hand(new URL(url, location.href).pathname, fieldsFromBody(init?.body));
              return Promise.resolve(ok());
            }
          } catch {
            /* fall through to their own fetch */
          }
          return theirFetch.apply(this, arguments);
        };
      }

      const theirOpen = win.XMLHttpRequest?.prototype?.open;
      const theirSend = win.XMLHttpRequest?.prototype?.send;
      if (theirOpen && theirSend) {
        win.XMLHttpRequest.prototype.open = function (method, url) {
          try {
            if (url && aimedAtCnl(url)) this.__klCnl = new URL(url, location.href).pathname;
          } catch {
            /* see above */
          }
          return theirOpen.apply(this, arguments);
        };
        win.XMLHttpRequest.prototype.send = function (body) {
          if (this.__klCnl) {
            hand(this.__klCnl, fieldsFromBody(body));
            return;
          }
          return theirSend.apply(this, arguments);
        };
      }

      const theirSubmit = win.HTMLFormElement?.prototype?.submit;
      if (theirSubmit) {
        win.HTMLFormElement.prototype.submit = function () {
          try {
            if (this.action && aimedAtCnl(this.action)) {
              hand(new URL(this.action, location.href).pathname, fieldsFromForm(this));
              return;
            }
          } catch {
            /* fall through */
          }
          return theirSubmit.apply(this, arguments);
        };
      }

      win.addEventListener(
        'submit',
        (e) => {
          try {
            const form = e.target;
            if (form?.action && aimedAtCnl(form.action)) {
              e.preventDefault();
              e.stopPropagation();
              hand(new URL(form.action, location.href).pathname, fieldsFromForm(form));
            }
          } catch {
            /* fall through */
          }
        },
        true,
      );
    } catch {
      // Cross-origin, or a window that closed between opening and this line.
      // Nothing to do and nothing worth breaking the page over.
    }
  };

  // sendBeacon: fire-and-forget by design, so a site using it never notices
  // that nothing listened.
  if (navigator.sendBeacon) {
    const realBeacon = navigator.sendBeacon.bind(navigator);
    navigator.sendBeacon = function (url, data) {
      try {
        if (url && aimedAtCnl(url)) {
          hand(new URL(url, location.href).pathname, fieldsFromBody(data));
          return true;
        }
      } catch {
        /* fall through */
      }
      return realBeacon(url, data);
    };
  }

  // An <iframe> or <img> pointed at the port: the GET-flavoured "/flash/add"
  // ping, still in the wild. The property setter is patched rather than the
  // attribute, because both spellings end up here.
  for (const [Ctor, name] of [
    [window.HTMLIFrameElement, 'HTMLIFrameElement'],
    [window.HTMLImageElement, 'HTMLImageElement'],
    // A <script src> aimed at the port is the same GET-flavoured ping, and it
    // was the one element path missing from this list. Sites that use it were
    // invisible to the whole interceptor.
    [window.HTMLScriptElement, 'HTMLScriptElement'],
  ]) {
    try {
      const desc = Ctor && Object.getOwnPropertyDescriptor(Ctor.prototype, 'src');
      if (!desc?.set) continue;
      Object.defineProperty(Ctor.prototype, 'src', {
        ...desc,
        set(value) {
          try {
            if (value && aimedAtCnl(value)) {
              const u = new URL(value, location.href);
              hand(u.pathname, Object.fromEntries(u.searchParams));
              // Left unset on purpose: pointing the element at a port nothing
              // listens on only produces a console error for a submission that
              // has already been handed over.
              return;
            }
          } catch {
            /* fall through to the real setter */
          }
          desc.set.call(this, value);
        },
      });
    } catch {
      // A browser that will not let this prototype be redefined. The other
      // paths still stand; name is kept for the reader, not for a log.
      void name;
    }
  }

  // A plain <a href="http://127.0.0.1:9666/flash/add?...">: no script, no form,
  // just a link. Capture phase so the page's own handler cannot stop it first,
  // and preventDefault only once the submission has actually been handed over.
  document.addEventListener(
    'click',
    (e) => {
      try {
        const a = e.target?.closest?.('a[href]');
        if (!a || !aimedAtCnl(a.href)) return;
        const u = new URL(a.href, location.href);
        hand(u.pathname, Object.fromEntries(u.searchParams));
        e.preventDefault();
        e.stopPropagation();
      } catch {
        /* let the click through rather than swallow it */
      }
    },
    true,
  );

  document.addEventListener(
    'submit',
    (e) => {
      try {
        const form = e.target;
        if (form?.action && aimedAtCnl(form.action)) {
          e.preventDefault();
          e.stopPropagation();
          hand(new URL(form.action, location.href).pathname, fieldsFromForm(form));
        }
      } catch {
        /* never block a submission we failed to understand */
      }
    },
    true,
  );
})();

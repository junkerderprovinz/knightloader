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
  // Answering it from here rather than by redirecting that request is worth a
  // paragraph, because the redirect was built first and then thrown away.
  // A declarativeNetRequest redirect needs host permission for the INITIATOR -
  // the website - not merely for the address being requested. Measured, not
  // read: with permission for 127.0.0.1:9666 alone the rule never fired and
  // the request went to the network; with <all_urls> it fired immediately.
  // That is why JDownloader's own extension declares <all_urls>.
  //
  // This file already runs in the page at document_start, before any script
  // the page brings. Declaring the global here costs no permission at all, so
  // the extension does not have to ask for access to every website in order to
  // answer one probe. The failed request still appears in the console; that is
  // the whole price, and it is a line of console noise rather than a
  // permission prompt on every install.
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
      return realOpen2.apply(this, arguments);
    };
  }

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

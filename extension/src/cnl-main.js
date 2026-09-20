/**
 * Catches a Click'n'Load submission inside the page, before it leaves.
 *
 * It runs in the page's main world because only code in that context sees the
 * page's fetch, XHR and forms. The main world has no chrome.* APIs, so every
 * catch goes by postMessage to cnl-relay.js in the isolated world, the same
 * split JDownloader's own MV3 extension uses.
 *
 * It runs at document_start, since a patch installed after the site captured
 * `fetch` never fires. The page gets the "success\r\n" a real JDownloader sends
 * (internal/cnl/cnl.go), so the button behaves as always and the user does not
 * click again.
 */
(() => {
  const HOSTS = ['127.0.0.1:9666', 'localhost:9666'];
  const TAG = 'knightloader-cnl';

  // Sites probe for JDownloader with <script src="http://127.0.0.1:9666/jdcheck.js">
  // before they show a Click'n'Load button. Declaring the globals here answers
  // every site that reads them; sites that go by the script's onerror are
  // answered by the redirect in cnl-rules.json.
  try {
    if (typeof window.jdownloader === 'undefined') {
      window.jdownloader = true;
      // Same number internal/cnl/cnl.go serves.
      if (typeof window.version === 'undefined') window.version = '90000';
    }
  } catch {
    // A frame that will not take the property; nothing else depends on it.
  }

  const aimedAtCnl = (raw) => {
    try {
      return HOSTS.includes(new URL(raw, location.href).host);
    } catch {
      return false;
    }
  };

  /** Hands one submission to the isolated world without making the page wait. */
  const hand = (path, fields) => {
    // "*" because location.origin does not match the window's own origin on
    // file:// pages, sandboxed iframes and data: URLs, and the message would be
    // dropped. It only reaches this window, the receiver checks event.source,
    // and the payload is the page's own submission.
    try {
      window.postMessage({ [TAG]: true, path, fields }, '*');
    } catch {
      // Nothing left to try.
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

  // Byte-identical to the Go server's reply.
  const ok = () => new Response('success\r\n', { status: 200, headers: { 'Content-Type': 'text/plain' } });

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
        // A bug here must not break the page's own fetch.
      }
      return realFetch.apply(this, arguments);
    };
  }

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
      // Complete the request the page waits for, or it reports a failure.
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

  // form.submit() fires no submit event, and a click on a submit button never
  // calls form.submit(), so both are caught.
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

  // Older sites reach port 9666 through window.open, sendBeacon, an element's
  // src or a plain link. The element cases patch the src setter rather than
  // observing the whole document, which is cheaper and catches the assignment
  // itself.

  const realOpen2 = window.open;
  if (typeof realOpen2 === 'function') {
    window.open = function (url, ...rest) {
      try {
        if (url && aimedAtCnl(url)) {
          const u = new URL(url, location.href);
          hand(u.pathname, Object.fromEntries(u.searchParams));
          // null would make the site think the popup was blocked.
          return window;
        }
      } catch {
        /* fall through to the real open */
      }
      const opened = realOpen2.apply(this, arguments);
      // A helper window submits from its own realm, so every opened window is
      // patched, and again on load because navigating brings new prototypes.
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
   * Patches a window the page just opened. A site that opens a helper window on
   * its own domain or about:blank and submits from inside it uses that window's
   * own fetch, XHR and forms, a realm this file never ran in, so the request
   * would reach a local JDownloader instead.
   *
   * Only the three paths a helper window uses are patched. Reaching into a
   * cross-origin window throws, so everything is wrapped. `hand` posts to this
   * window, where the relay listens.
   */
  const installInOpened = (win) => {
    if (!win || win === window) return;
    try {
      // 'load' can fire more than once for one window.
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
      // Cross-origin, or the window closed in the meantime.
    }
  };

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

  // An <iframe>, <img> or <script> pointed at the port sends the GET
  // "/flash/add" ping.
  for (const [Ctor, name] of [
    [window.HTMLIFrameElement, 'HTMLIFrameElement'],
    [window.HTMLImageElement, 'HTMLImageElement'],
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
              // Left unset: the port has no listener, so it would only log an
              // error.
              return;
            }
          } catch {
            /* fall through to the real setter */
          }
          desc.set.call(this, value);
        },
      });
    } catch {
      // This prototype cannot be redefined; the other paths still work.
      void name;
    }
  }

  // A plain <a href="http://127.0.0.1:9666/flash/add?...">. Capture phase so the
  // page's own handler cannot stop it first.
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

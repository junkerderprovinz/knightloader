// jsdom lays nothing out and has no ResizeObserver, which every horizontal
// strip watches its box with. A test that measures a strip stubs its own.
if (!('ResizeObserver' in globalThis)) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}

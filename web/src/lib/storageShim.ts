// Opened through the fortress, the interface runs in a sandboxed frame with an
// origin of its own, where touching localStorage, sessionStorage or
// document.cookie throws. Stores in memory stand in for them there, so the
// choices of the visit last as long as the tab. Where storage works, nothing
// changes. main.tsx imports this first, before any module reads a store.

class MemoryStorage implements Storage {
  private items = new Map<string, string>();

  get length(): number {
    return this.items.size;
  }

  clear(): void {
    this.items.clear();
  }

  getItem(key: string): string | null {
    return this.items.get(String(key)) ?? null;
  }

  key(index: number): string | null {
    return [...this.items.keys()][index] ?? null;
  }

  removeItem(key: string): void {
    this.items.delete(String(key));
  }

  setItem(key: string, value: string): void {
    this.items.set(String(key), String(value));
  }
}

function usable(read: () => unknown): boolean {
  try {
    read();
    return true;
  } catch {
    return false;
  }
}

/** standInForStorage puts memory where a store of the window throws. */
export function standInForStorage(win: Window = window, doc: Document = document): void {
  for (const name of ['localStorage', 'sessionStorage'] as const) {
    if (usable(() => win[name].length)) continue;
    Object.defineProperty(win, name, { value: new MemoryStorage(), configurable: true });
  }
  if (!usable(() => doc.cookie)) {
    const cookies = new Map<string, string>();
    Object.defineProperty(doc, 'cookie', {
      configurable: true,
      get: () => [...cookies].map(([name, value]) => `${name}=${value}`).join('; '),
      set: (v: string) => {
        const [pair] = String(v).split(';');
        const at = pair.indexOf('=');
        cookies.set(pair.slice(0, at < 0 ? 0 : at).trim(), pair.slice(at + 1).trim());
      },
    });
  }
}

standInForStorage();

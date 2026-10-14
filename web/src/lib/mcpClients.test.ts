// A client row stores its client's id, so an id that changes or collides would
// put the wrong mark on clients already set up. The rest keeps a tile from
// opening a window with nothing to copy or a mark that is not there.
import { describe, expect, it } from 'vitest';
import { CLIENT_MARKS } from './mcpClientMarks';
import { CLOUD_CLIENTS, LOCAL_CLIENTS, OTHER_CLIENT, clientById, snippetFor } from './mcpClients';
import { SNIPPETS } from './mcpSnippets';

const ALL = [...LOCAL_CLIENTS, ...CLOUD_CLIENTS, OTHER_CLIENT];
const input = { origin: 'http://tower:8080/kl', endpointPath: '/mcp', token: '<your token>' };

describe('the client list', () => {
  it('gives every client an id the server stores', () => {
    const ids = ALL.map((c) => c.id);
    expect(new Set(ids).size).toBe(ids.length);
    for (const id of ids) expect(id).toMatch(/^[a-z0-9][a-z0-9-]{0,31}$/);
  });

  // check-tile-hover.mjs holds each tile class to index.css.
  it('has a mark for every tile', () => {
    for (const c of [...LOCAL_CLIENTS, ...CLOUD_CLIENTS]) expect(CLIENT_MARKS[c.mark ?? ''], c.id).toBeDefined();
  });

  it('has a configuration of its own for every client, naming the address under the base path', () => {
    for (const c of ALL) {
      expect(c.id in SNIPPETS, c.id).toBe(true);
      expect(snippetFor(c, input), c.id).toContain('http://tower:8080/kl/mcp');
    }
  });

  it('keeps the cloud clients apart, since only they need the internet', () => {
    expect(CLOUD_CLIENTS.every((c) => c.group === 'cloud')).toBe(true);
    expect(LOCAL_CLIENTS.every((c) => c.group === 'local')).toBe(true);
  });

  it('falls back to Other client for an id the list has dropped', () => {
    expect(clientById('cursor').name).toBe('Cursor');
    expect(clientById('gone')).toBe(OTHER_CLIENT);
  });
});

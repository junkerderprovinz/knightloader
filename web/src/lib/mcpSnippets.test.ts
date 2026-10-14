// These snippets are pasted where KnightLoader cannot see them, so a wrong
// header or a missing --allow-http surfaces as a client that fails to connect
// for no visible reason.
import { describe, expect, it } from 'vitest';
import {
  SNIPPETS,
  TOKEN_FILE_PLACEHOLDER,
  TOKEN_VARIABLE,
  claudeCodeSnippet,
  claudeDesktopSnippet,
  mcpUrl,
  type McpSnippetClient,
  type McpSnippetInput,
} from './mcpSnippets';

const TOKEN = 'kl_7Qa-Zb0_cD';
const plain: McpSnippetInput = { origin: 'http://192.168.1.10:8080', endpointPath: '/mcp', token: TOKEN };
const proxied: McpSnippetInput = { ...plain, origin: 'https://kl.example.com' };

/** The clients that read the token from a file, a variable or a prompt. */
const KEEPS_THE_TOKEN_ELSEWHERE: McpSnippetClient[] = [
  'antigravity',
  'claude-code',
  'claude-desktop',
  'codex',
  'continue',
  'cursor',
  'gemini',
  'copilot',
  'goose',
  'vibe',
  'opencode',
  'perplexity',
  'qwen',
  'roo',
  'windsurf',
];

describe('mcpUrl', () => {
  it('joins the origin and the path with one slash', () => {
    expect(mcpUrl({ origin: 'http://tower:8080/kl/', endpointPath: '/mcp' })).toBe('http://tower:8080/kl/mcp');
  });
});

describe('the snippets', () => {
  it('never hold the token where the client can read it from elsewhere', () => {
    for (const client of KEEPS_THE_TOKEN_ELSEWHERE) expect(SNIPPETS[client](plain), client).not.toContain(TOKEN);
  });

  it('hold the token where the client has no other way', () => {
    for (const client of Object.keys(SNIPPETS) as McpSnippetClient[]) {
      if (KEEPS_THE_TOKEN_ELSEWHERE.includes(client)) continue;
      expect(SNIPPETS[client](plain), client).toContain(TOKEN);
    }
  });

  it('name the variable the window tells the reader to set', () => {
    for (const client of ['cursor', 'codex', 'gemini', 'windsurf'] as const) {
      expect(SNIPPETS[client](plain), client).toContain(TOKEN_VARIABLE);
    }
  });

  it('let mcp-remote use plain http only where the address is plain http', () => {
    expect(claudeCodeSnippet(plain)).toContain('--allow-http');
    expect(claudeCodeSnippet(proxied)).not.toContain('--allow-http');
    const args = (JSON.parse(claudeDesktopSnippet(plain)) as { mcpServers: { knightloader: { args: string[] } } })
      .mcpServers.knightloader.args;
    expect(args).toContain('--allow-http');
    expect(args).toContain(TOKEN_FILE_PLACEHOLDER);
  });

  it('parse where they are JSON', () => {
    for (const client of ['anythingllm', 'cline', 'copilot-cli', 'cursor', 'zed', 'visualstudio'] as const) {
      expect(() => JSON.parse(SNIPPETS[client](plain)), client).not.toThrow();
    }
  });
});

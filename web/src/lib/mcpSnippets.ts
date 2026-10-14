// The configuration each MCP client needs, as text to copy. Pure functions with
// no i18n: every line is a command, a header, a form field or a key that a
// client parses literally, so translating any of it would break the connection.
// The shapes are BombVault's, so a client set up for one app reads the same for
// the other.
//
// A token never goes on a command line, where the process list and the shell
// history would keep it. Where a client can take it from a file, an environment
// variable or a secret prompt, its snippet names that instead; where it cannot,
// the token sits in the snippet and the window says so.

export interface McpSnippetInput {
  /** Scheme, host, port and base path of the address KnightLoader was opened at. */
  origin: string;
  endpointPath: string;
  /** The fresh token, or TOKEN_PLACEHOLDER outside the window that shows it once. */
  token: string;
}

export const TOKEN_PLACEHOLDER = '<your token>';
export const TOKEN_FILE_PLACEHOLDER = '<path of the file with your token>';

/** The environment variable every client that reads one takes the token from. */
export const TOKEN_VARIABLE = 'KNIGHTLOADER_MCP_TOKEN';

/** The name each configuration files the server under. */
const NAME = 'knightloader';

/** mcpUrl joins the origin and the endpoint path without doubling the slash
 *  between them. */
export function mcpUrl(i: Pick<McpSnippetInput, 'origin' | 'endpointPath'>): string {
  return i.origin.replace(/\/+$/, '') + '/' + i.endpointPath.replace(/^\/+/, '');
}

function plainHttp(i: McpSnippetInput): boolean {
  return i.origin.toLowerCase().startsWith('http:');
}

function json(value: unknown): string {
  return JSON.stringify(value, null, 2);
}

/**
 * mcpRemoteCommand starts mcp-remote with the token read from a file, so
 * neither the command nor the process list shows it. A `${VAR}` in the
 * arguments would not keep it out: Claude Code fills it from its own
 * environment before it starts the server. `@latest` keeps an older global
 * mcp-remote without `--header-file` from being picked, and the path is quoted
 * because it is filled in by hand and a space would split it.
 */
function mcpRemoteCommand(i: McpSnippetInput): string {
  const http = plainHttp(i) ? ' --allow-http' : '';
  return `npx -y mcp-remote@latest ${mcpUrl(i)} --header-file "${TOKEN_FILE_PLACEHOLDER}"${http}`;
}

export function claudeCodeSnippet(i: McpSnippetInput): string {
  return `claude mcp add ${NAME} --scope user -- ${mcpRemoteCommand(i)}`;
}

/** claudeDesktopSnippet names the token file as the Claude Code command does,
 *  so the JSON holds no token. */
export function claudeDesktopSnippet(i: McpSnippetInput): string {
  const args = ['-y', 'mcp-remote@latest', mcpUrl(i), '--header-file', TOKEN_FILE_PLACEHOLDER];
  if (plainHttp(i)) args.push('--allow-http');
  return json({ mcpServers: { [NAME]: { command: 'npx', args } } });
}

/** genericSnippet lists the fields any Streamable HTTP client asks for, with
 *  both header forms. */
export function genericSnippet(i: McpSnippetInput): string {
  return [
    `URL: ${mcpUrl(i)}`,
    'Transport: Streamable HTTP',
    `Authorization: Bearer ${i.token}`,
    `X-API-Key: ${i.token}`,
  ].join('\n');
}

/** The header of a client that holds the token itself. */
function bearer(i: McpSnippetInput) {
  return { Authorization: `Bearer ${i.token}` };
}

/** The header of a client that fills in the variable, in its own syntax. */
function bearerFrom(variable: string) {
  return { Authorization: `Bearer ${variable}` };
}

/** A form typed into the client's own window, one field per line, named as
 *  the client names them. */
function form(fields: [string, string][]): string {
  return fields.map(([name, value]) => `${name}: ${value}`).join('\n');
}

/** SNIPPETS holds each client's snippet under its id in the client list. */
export const SNIPPETS = {
  anythingllm: (i: McpSnippetInput) =>
    json({ mcpServers: { [NAME]: { type: 'streamable', url: mcpUrl(i), headers: bearer(i) } } }),
  antigravity: (i: McpSnippetInput) =>
    json({ mcpServers: { [NAME]: { serverUrl: mcpUrl(i), headers: bearerFrom(`\${${TOKEN_VARIABLE}}`) } } }),
  'claude-code': claudeCodeSnippet,
  'claude-desktop': claudeDesktopSnippet,
  cline: (i: McpSnippetInput) =>
    json({ mcpServers: { [NAME]: { type: 'streamableHttp', url: mcpUrl(i), headers: bearer(i), disabled: false } } }),
  codex: (i: McpSnippetInput) =>
    [`[mcp_servers.${NAME}]`, `url = "${mcpUrl(i)}"`, `bearer_token_env_var = "${TOKEN_VARIABLE}"`].join('\n'),
  continue: (i: McpSnippetInput) =>
    [
      'mcpServers:',
      `  - name: ${NAME}`,
      '    type: streamable-http',
      `    url: ${mcpUrl(i)}`,
      '    requestOptions:',
      '      headers:',
      `        Authorization: "Bearer \${{ secrets.${TOKEN_VARIABLE} }}"`,
    ].join('\n'),
  'copilot-cli': (i: McpSnippetInput) =>
    json({ mcpServers: { [NAME]: { type: 'http', url: mcpUrl(i), headers: bearer(i), tools: ['*'] } } }),
  cursor: (i: McpSnippetInput) =>
    json({ mcpServers: { [NAME]: { url: mcpUrl(i), headers: bearerFrom(`\${env:${TOKEN_VARIABLE}}`) } } }),
  gemini: (i: McpSnippetInput) =>
    json({ mcpServers: { [NAME]: { httpUrl: mcpUrl(i), headers: bearerFrom(`\${${TOKEN_VARIABLE}}`) } } }),
  copilot: (i: McpSnippetInput) =>
    json({
      inputs: [{ type: 'promptString', id: 'knightloader-token', description: 'KnightLoader MCP token', password: true }],
      servers: { [NAME]: { type: 'http', url: mcpUrl(i), headers: bearerFrom('${input:knightloader-token}') } },
    }),
  goose: (i: McpSnippetInput) =>
    [
      'extensions:',
      `  ${NAME}:`,
      '    type: streamable_http',
      `    name: ${NAME}`,
      '    enabled: true',
      `    uri: "${mcpUrl(i)}"`,
      '    headers:',
      `      Authorization: "Bearer \${${TOKEN_VARIABLE}}"`,
      '    env_keys:',
      `      - ${TOKEN_VARIABLE}`,
      '    timeout: 300',
    ].join('\n'),
  jan: (i: McpSnippetInput) =>
    form([
      ['Transport', 'Streamable HTTP'],
      ['URL', mcpUrl(i)],
      ['Header', 'Authorization'],
      ['Value', `Bearer ${i.token}`],
    ]),
  jetbrains: (i: McpSnippetInput) => json({ mcpServers: { [NAME]: { url: mcpUrl(i), headers: bearer(i) } } }),
  kimi: (i: McpSnippetInput) => json({ mcpServers: { [NAME]: { url: mcpUrl(i), headers: bearer(i) } } }),
  lmstudio: (i: McpSnippetInput) => json({ mcpServers: { [NAME]: { url: mcpUrl(i), headers: bearer(i) } } }),
  vibe: (i: McpSnippetInput) =>
    [
      '[[mcp_servers]]',
      `name = "${NAME}"`,
      'transport = "streamable-http"',
      `url = "${mcpUrl(i)}"`,
      `api_key_env = "${TOKEN_VARIABLE}"`,
      'api_key_header = "Authorization"',
      'api_key_format = "Bearer {token}"',
    ].join('\n'),
  msty: (i: McpSnippetInput) =>
    form([
      ['Name', NAME],
      ['Connection Type', 'HTTP'],
      ['Server URL', mcpUrl(i)],
      ['Authentication', 'Bearer'],
      ['Token', i.token],
    ]),
  n8n: (i: McpSnippetInput) =>
    form([
      ['Endpoint URL', mcpUrl(i)],
      ['Server Transport', 'HTTP Streamable'],
      ['Authentication', 'Bearer Auth'],
      ['Credential, Token', i.token],
    ]),
  openwebui: (i: McpSnippetInput) =>
    form([
      ['Type', 'MCP (Streamable HTTP)'],
      ['URL', mcpUrl(i)],
      ['Auth', 'Bearer'],
      ['Key', i.token],
    ]),
  opencode: (i: McpSnippetInput) =>
    json({
      $schema: 'https://opencode.ai/config.json',
      mcp: { [NAME]: { type: 'remote', url: mcpUrl(i), enabled: true, headers: bearerFrom(`{env:${TOKEN_VARIABLE}}`) } },
    }),
  perplexity: (i: McpSnippetInput) =>
    form([
      ['Server Name', NAME],
      ['Command', mcpRemoteCommand(i)],
    ]),
  qwen: (i: McpSnippetInput) =>
    json({ mcpServers: { [NAME]: { httpUrl: mcpUrl(i), headers: bearerFrom(`\${${TOKEN_VARIABLE}}`) } } }),
  roo: (i: McpSnippetInput) =>
    json({
      mcpServers: { [NAME]: { type: 'streamable-http', url: mcpUrl(i), headers: bearerFrom(`\${env:${TOKEN_VARIABLE}}`) } },
    }),
  visualstudio: (i: McpSnippetInput) =>
    json({ servers: { [NAME]: { url: mcpUrl(i), requestInit: { headers: bearer(i) } } } }),
  warp: (i: McpSnippetInput) => json({ mcpServers: { [NAME]: { url: mcpUrl(i), headers: bearer(i) } } }),
  windsurf: (i: McpSnippetInput) =>
    json({ mcpServers: { [NAME]: { serverUrl: mcpUrl(i), headers: bearerFrom(`\${env:${TOKEN_VARIABLE}}`) } } }),
  zed: (i: McpSnippetInput) => json({ context_servers: { [NAME]: { url: mcpUrl(i), headers: bearer(i) } } }),
  grok: (i: McpSnippetInput) =>
    form([
      ['MCP server URL', mcpUrl(i)],
      ['Header', 'Authorization'],
      ['Value', `Bearer ${i.token}`],
    ]),
  lechat: (i: McpSnippetInput) =>
    form([
      ['Server URL', mcpUrl(i)],
      ['Authentication', 'HTTP Bearer Token'],
      ['Token', i.token],
    ]),
  other: genericSnippet,
} satisfies Record<string, (i: McpSnippetInput) => string>;

export type McpSnippetClient = keyof typeof SNIPPETS;

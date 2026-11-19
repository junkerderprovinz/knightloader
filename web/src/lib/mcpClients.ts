// The MCP clients the settings card offers a tile for, and what each one's
// setup window says. The list and its facts are BombVault's, taken from each
// client's own documentation: where its configuration lives and how it can get
// the token without holding it. ChatGPT and Claude on claude.ai are missing
// because they sign in through OAuth, which KnightLoader does not serve.
import type { TranslationKey } from './i18n';
import { SNIPPETS, type McpSnippetClient, type McpSnippetInput } from './mcpSnippets';

/** Where a configuration file lives. `unix` stands for macOS and Linux alike. */
export interface ConfigPaths {
  windows?: string;
  macos?: string;
  linux?: string;
  unix?: string;
}

/** How the configuration gets into the client. */
export type SetupWay =
  | { kind: 'command' }
  /** `ui` opens the file in the client; `paste` takes the same text pasted in. */
  | { kind: 'file'; paths: ConfigPaths; ui?: string; paste?: string }
  | { kind: 'form'; ui: string }
  | { kind: 'other' };

/** How the client comes by the token. */
export type TokenWay =
  /** mcp-remote reads it from a file the configuration names. */
  | { kind: 'tokenFile' }
  /** An environment variable. `unsure` where the documentation shows the
   *  variable in the file's other fields but not in a header. */
  | { kind: 'env'; unsure?: boolean }
  /** A masked prompt, kept in the client's secret storage. */
  | { kind: 'prompt' }
  /** A line in a dotenv file of the client's own. */
  | { kind: 'dotenv'; file: string }
  | { kind: 'inFile' }
  | { kind: 'inApp' }
  /** An encrypted credential store. */
  | { kind: 'credential' }
  /** The vendor's servers keep it. */
  | { kind: 'vendor' }
  | { kind: 'other' };

export type ClientGroup = 'local' | 'cloud';

export interface McpClient {
  /** Stored with the client set up here, so it must never change. */
  id: string;
  name: string;
  group: ClientGroup;
  setup: SetupWay;
  token: TokenWay;
  /** A sentence the window puts first. */
  note?: TranslationKey;
  /** The client runs mcp-remote through npx. */
  node?: boolean;
}

/** VS Code's user data folder, where its extensions keep their settings. */
function vsCodeStorage(rest: string): ConfigPaths {
  return {
    windows: `%APPDATA%\\Code\\User\\${rest.replaceAll('/', '\\')}`,
    macos: `~/Library/Application Support/Code/User/${rest}`,
    linux: `~/.config/Code/User/${rest}`,
  };
}

/** A file in the user's home folder. */
function home(rest: string): ConfigPaths {
  return { windows: `%USERPROFILE%\\${rest.replaceAll('/', '\\')}`, unix: `~/${rest}` };
}

export const LOCAL_CLIENTS: McpClient[] = [
  {
    id: 'anythingllm',
    name: 'AnythingLLM',
    group: 'local',
    setup: {
      kind: 'file',
      paths: {
        windows: '%APPDATA%\\anythingllm-desktop\\storage\\plugins\\anythingllm_mcp_servers.json',
        macos: '~/Library/Application Support/anythingllm-desktop/storage/plugins/anythingllm_mcp_servers.json',
        linux: '~/.config/anythingllm-desktop/storage/plugins/anythingllm_mcp_servers.json',
      },
      ui: 'Agent Skills, MCP Servers',
    },
    token: { kind: 'inFile' },
  },
  {
    id: 'antigravity',
    name: 'Antigravity',
    group: 'local',
    setup: { kind: 'file', paths: home('.gemini/config/mcp_config.json'), ui: 'Manage MCP Servers, View raw config' },
    token: { kind: 'env' },
  },
  {
    id: 'claude-code',
    name: 'Claude Code',
    group: 'local',
    setup: { kind: 'command' },
    token: { kind: 'tokenFile' },
    node: true,
  },
  {
    id: 'claude-desktop',
    name: 'Claude Desktop',
    group: 'local',
    setup: {
      kind: 'file',
      paths: {
        windows: '%APPDATA%\\Claude\\claude_desktop_config.json',
        macos: '~/Library/Application Support/Claude/claude_desktop_config.json',
      },
      ui: 'Settings, Developer, Edit Config',
    },
    token: { kind: 'tokenFile' },
    node: true,
  },
  {
    id: 'cline',
    name: 'Cline',
    group: 'local',
    setup: {
      kind: 'file',
      paths: vsCodeStorage('globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json'),
      ui: 'MCP Servers, Configure MCP Servers',
    },
    token: { kind: 'inFile' },
  },
  {
    id: 'codex',
    name: 'Codex CLI',
    group: 'local',
    setup: { kind: 'file', paths: home('.codex/config.toml') },
    token: { kind: 'env' },
  },
  {
    id: 'continue',
    name: 'Continue',
    group: 'local',
    setup: { kind: 'file', paths: home('.continue/config.yaml') },
    token: { kind: 'dotenv', file: '~/.continue/.env' },
  },
  {
    id: 'copilot-cli',
    name: 'Copilot CLI',
    group: 'local',
    setup: { kind: 'file', paths: home('.copilot/mcp-config.json') },
    token: { kind: 'inFile' },
  },
  {
    id: 'cursor',
    name: 'Cursor',
    group: 'local',
    setup: { kind: 'file', paths: home('.cursor/mcp.json'), ui: 'Settings, MCP, Add new global MCP server' },
    token: { kind: 'env' },
  },
  {
    id: 'gemini',
    name: 'Gemini CLI',
    group: 'local',
    setup: { kind: 'file', paths: home('.gemini/settings.json') },
    token: { kind: 'env', unsure: true },
  },
  {
    id: 'copilot',
    name: 'GitHub Copilot',
    group: 'local',
    setup: { kind: 'file', paths: vsCodeStorage('mcp.json'), ui: 'MCP: Open User Configuration' },
    token: { kind: 'prompt' },
  },
  {
    id: 'goose',
    name: 'Goose',
    group: 'local',
    setup: {
      kind: 'file',
      paths: { windows: '%APPDATA%\\Block\\goose\\config\\config.yaml', unix: '~/.config/goose/config.yaml' },
    },
    token: { kind: 'env', unsure: true },
  },
  {
    id: 'jan',
    name: 'Jan',
    group: 'local',
    setup: { kind: 'form', ui: 'Settings, MCP Servers, Add MCP Server' },
    token: { kind: 'inApp' },
  },
  {
    id: 'jetbrains',
    name: 'JetBrains',
    group: 'local',
    setup: {
      kind: 'file',
      paths: home('.junie/mcp/mcp.json'),
      ui: 'Settings, Tools, Junie, MCP Settings',
      paste: 'Settings, Tools, AI Assistant, Model Context Protocol (MCP), Add',
    },
    token: { kind: 'inFile' },
  },
  {
    id: 'kimi',
    name: 'Kimi Code',
    group: 'local',
    setup: { kind: 'file', paths: home('.kimi/mcp.json') },
    token: { kind: 'inFile' },
  },
  {
    id: 'lmstudio',
    name: 'LM Studio',
    group: 'local',
    setup: { kind: 'file', paths: home('.lmstudio/mcp.json'), ui: 'Program, Install, Edit mcp.json' },
    token: { kind: 'inFile' },
  },
  {
    id: 'vibe',
    name: 'Mistral Vibe',
    group: 'local',
    setup: { kind: 'file', paths: home('.vibe/config.toml') },
    token: { kind: 'env' },
  },
  {
    id: 'msty',
    name: 'Msty',
    group: 'local',
    setup: { kind: 'form', ui: 'Toolbox, Tools, Add New Tool' },
    token: { kind: 'inApp' },
  },
  {
    id: 'n8n',
    name: 'n8n',
    group: 'local',
    setup: { kind: 'form', ui: 'MCP Client Tool' },
    token: { kind: 'credential' },
  },
  {
    id: 'openwebui',
    name: 'Open WebUI',
    group: 'local',
    setup: { kind: 'form', ui: 'Admin Panel, Settings, External Tools, Add Connection' },
    token: { kind: 'inApp' },
  },
  {
    id: 'opencode',
    name: 'opencode',
    group: 'local',
    setup: { kind: 'file', paths: home('.config/opencode/opencode.json') },
    token: { kind: 'env' },
  },
  {
    id: 'perplexity',
    name: 'Perplexity',
    group: 'local',
    setup: { kind: 'form', ui: 'Settings, Connectors, Add Connector, MCP Connector' },
    token: { kind: 'tokenFile' },
    note: 'settings.access.mcp.notePerplexity',
    node: true,
  },
  {
    id: 'qwen',
    name: 'Qwen Code',
    group: 'local',
    setup: { kind: 'file', paths: home('.qwen/settings.json') },
    token: { kind: 'env', unsure: true },
  },
  {
    id: 'roo',
    name: 'Roo Code',
    group: 'local',
    setup: {
      kind: 'file',
      paths: vsCodeStorage('globalStorage/rooveterinaryinc.roo-cline/settings/mcp_settings.json'),
      ui: 'MCP, Edit Global MCP',
    },
    token: { kind: 'env' },
  },
  {
    id: 'visualstudio',
    name: 'Visual Studio',
    group: 'local',
    setup: { kind: 'file', paths: { windows: '%USERPROFILE%\\.mcp.json' } },
    token: { kind: 'inFile' },
  },
  {
    id: 'warp',
    name: 'Warp',
    group: 'local',
    setup: {
      kind: 'file',
      paths: { unix: '~/.warp/.mcp.json' },
      paste: 'Settings, Agents, Warp Agent, Manage MCP servers, + Add',
    },
    token: { kind: 'inFile' },
  },
  {
    id: 'windsurf',
    name: 'Windsurf',
    group: 'local',
    setup: {
      kind: 'file',
      paths: { windows: '%APPDATA%\\devin\\mcp_config.json', unix: '~/.config/devin/mcp_config.json' },
    },
    token: { kind: 'env' },
  },
  {
    id: 'zed',
    name: 'Zed',
    group: 'local',
    setup: {
      kind: 'file',
      paths: { windows: '%APPDATA%\\Zed\\settings.json', unix: '~/.config/zed/settings.json' },
    },
    token: { kind: 'inFile' },
  },
];

export const CLOUD_CLIENTS: McpClient[] = [
  {
    id: 'grok',
    name: 'Grok',
    group: 'cloud',
    setup: { kind: 'form', ui: 'grok.com, Connectors, New Connector, Custom' },
    token: { kind: 'vendor' },
    note: 'settings.access.mcp.noteGrok',
  },
  {
    id: 'lechat',
    name: 'Le Chat',
    group: 'cloud',
    setup: { kind: 'form', ui: 'Connectors, Add a Custom Connector' },
    token: { kind: 'vendor' },
  },
];

export const OTHER_CLIENT: McpClient = {
  id: 'other',
  name: '',
  group: 'local',
  setup: { kind: 'other' },
  token: { kind: 'other' },
};

const BY_ID = new Map<string, McpClient>([...LOCAL_CLIENTS, ...CLOUD_CLIENTS, OTHER_CLIENT].map((c) => [c.id, c]));

/** The client a row was set up for; Other client for an id the list has dropped. */
export function clientById(id: string): McpClient {
  return BY_ID.get(id) ?? OTHER_CLIENT;
}

/** The configuration to copy for a client. */
export function snippetFor(client: McpClient, input: McpSnippetInput): string {
  return SNIPPETS[(client.id in SNIPPETS ? client.id : 'other') as McpSnippetClient](input);
}

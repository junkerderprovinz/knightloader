// The MCP clients the settings card offers a tile for, and what each one's
// setup window says. The list and its facts are BombVault's, taken from each
// client's own documentation: where its configuration lives and how it can get
// the token without holding it. ChatGPT and Claude on claude.ai are missing
// because they sign in through OAuth, which KnightLoader does not serve.
import type { TranslationKey } from './i18n';
import { SNIPPETS, type McpSnippetClient, type McpSnippetInput } from './mcpSnippets';

/** The second line of a client's tile. */
export type ClientKind = 'terminal' | 'editor' | 'desktop' | 'vscode' | 'localModels' | 'automation' | 'webChat';

export const KIND_LABEL: Record<ClientKind, TranslationKey> = {
  terminal: 'settings.access.mcp.kind.terminal',
  editor: 'settings.access.mcp.kind.editor',
  desktop: 'settings.access.mcp.kind.desktop',
  vscode: 'settings.access.mcp.kind.vscode',
  localModels: 'settings.access.mcp.kind.localModels',
  automation: 'settings.access.mcp.kind.automation',
  webChat: 'settings.access.mcp.kind.webChat',
};

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
  kind?: ClientKind;
  group: ClientGroup;
  /** The key into CLIENT_MARKS; Other client draws a glyph. */
  mark?: string;
  /** The `.glim-tile-*` class its tile lights up in. */
  tile: string;
  /** Brightens the resting mark on the dark theme, where its colour would
   *  read as a hole in the tile. */
  lift?: boolean;
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
    kind: 'localModels',
    group: 'local',
    mark: 'anythingllm',
    tile: 'glim-tile-anythingllm',
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
    kind: 'editor',
    group: 'local',
    mark: 'antigravity',
    tile: 'glim-tile-antigravity',
    setup: { kind: 'file', paths: home('.gemini/config/mcp_config.json'), ui: 'Manage MCP Servers, View raw config' },
    token: { kind: 'env' },
  },
  {
    id: 'claude-code',
    name: 'Claude Code',
    kind: 'terminal',
    group: 'local',
    mark: 'claudecode',
    tile: 'glim-tile-claude',
    setup: { kind: 'command' },
    token: { kind: 'tokenFile' },
    node: true,
  },
  {
    id: 'claude-desktop',
    name: 'Claude Desktop',
    kind: 'desktop',
    group: 'local',
    mark: 'claude',
    tile: 'glim-tile-claude',
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
    kind: 'vscode',
    group: 'local',
    mark: 'cline',
    tile: 'glim-tile-cline',
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
    kind: 'terminal',
    group: 'local',
    mark: 'openai',
    tile: 'glim-tile-openai',
    setup: { kind: 'file', paths: home('.codex/config.toml') },
    token: { kind: 'env' },
  },
  {
    id: 'continue',
    name: 'Continue',
    kind: 'vscode',
    group: 'local',
    mark: 'continue',
    tile: 'glim-tile-continue',
    setup: { kind: 'file', paths: home('.continue/config.yaml') },
    token: { kind: 'dotenv', file: '~/.continue/.env' },
  },
  {
    id: 'copilot-cli',
    name: 'Copilot CLI',
    kind: 'terminal',
    group: 'local',
    mark: 'copilot',
    tile: 'glim-tile-copilot',
    setup: { kind: 'file', paths: home('.copilot/mcp-config.json') },
    token: { kind: 'inFile' },
  },
  {
    id: 'cursor',
    name: 'Cursor',
    kind: 'editor',
    group: 'local',
    mark: 'cursor',
    tile: 'glim-tile-cursor',
    setup: { kind: 'file', paths: home('.cursor/mcp.json'), ui: 'Settings, MCP, Add new global MCP server' },
    token: { kind: 'env' },
  },
  {
    id: 'gemini',
    name: 'Gemini CLI',
    kind: 'terminal',
    group: 'local',
    mark: 'gemini',
    tile: 'glim-tile-gemini',
    setup: { kind: 'file', paths: home('.gemini/settings.json') },
    token: { kind: 'env', unsure: true },
  },
  {
    id: 'copilot',
    name: 'GitHub Copilot',
    kind: 'vscode',
    group: 'local',
    mark: 'vscode',
    tile: 'glim-tile-vscode',
    setup: { kind: 'file', paths: vsCodeStorage('mcp.json'), ui: 'MCP: Open User Configuration' },
    token: { kind: 'prompt' },
  },
  {
    id: 'goose',
    name: 'Goose',
    kind: 'desktop',
    group: 'local',
    mark: 'goose',
    tile: 'glim-tile-goose',
    setup: {
      kind: 'file',
      paths: { windows: '%APPDATA%\\Block\\goose\\config\\config.yaml', unix: '~/.config/goose/config.yaml' },
    },
    token: { kind: 'env', unsure: true },
  },
  {
    id: 'jan',
    name: 'Jan',
    kind: 'localModels',
    group: 'local',
    mark: 'jan',
    tile: 'glim-tile-jan',
    setup: { kind: 'form', ui: 'Settings, MCP Servers, Add MCP Server' },
    token: { kind: 'inApp' },
  },
  {
    id: 'jetbrains',
    name: 'JetBrains',
    kind: 'editor',
    group: 'local',
    mark: 'jetbrains',
    tile: 'glim-tile-jetbrains',
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
    kind: 'terminal',
    group: 'local',
    mark: 'kimi',
    tile: 'glim-tile-kimi',
    setup: { kind: 'file', paths: home('.kimi/mcp.json') },
    token: { kind: 'inFile' },
  },
  {
    id: 'lmstudio',
    name: 'LM Studio',
    kind: 'localModels',
    group: 'local',
    mark: 'lmstudio',
    tile: 'glim-tile-lmstudio',
    setup: { kind: 'file', paths: home('.lmstudio/mcp.json'), ui: 'Program, Install, Edit mcp.json' },
    token: { kind: 'inFile' },
  },
  {
    id: 'vibe',
    name: 'Mistral Vibe',
    kind: 'terminal',
    group: 'local',
    mark: 'mistral',
    tile: 'glim-tile-mistral',
    setup: { kind: 'file', paths: home('.vibe/config.toml') },
    token: { kind: 'env' },
  },
  {
    id: 'msty',
    name: 'Msty',
    kind: 'localModels',
    group: 'local',
    mark: 'msty',
    tile: 'glim-tile-msty',
    lift: true,
    setup: { kind: 'form', ui: 'Toolbox, Tools, Add New Tool' },
    token: { kind: 'inApp' },
  },
  {
    id: 'n8n',
    name: 'n8n',
    kind: 'automation',
    group: 'local',
    mark: 'n8n',
    tile: 'glim-tile-n8n',
    setup: { kind: 'form', ui: 'MCP Client Tool' },
    token: { kind: 'credential' },
  },
  {
    id: 'openwebui',
    name: 'Open WebUI',
    kind: 'localModels',
    group: 'local',
    mark: 'openwebui',
    tile: 'glim-tile-openwebui',
    setup: { kind: 'form', ui: 'Admin Panel, Settings, External Tools, Add Connection' },
    token: { kind: 'inApp' },
  },
  {
    id: 'opencode',
    name: 'opencode',
    kind: 'terminal',
    group: 'local',
    mark: 'opencode',
    tile: 'glim-tile-opencode',
    setup: { kind: 'file', paths: home('.config/opencode/opencode.json') },
    token: { kind: 'env' },
  },
  {
    id: 'perplexity',
    name: 'Perplexity',
    kind: 'desktop',
    group: 'local',
    mark: 'perplexity',
    tile: 'glim-tile-perplexity',
    setup: { kind: 'form', ui: 'Settings, Connectors, Add Connector, MCP Connector' },
    token: { kind: 'tokenFile' },
    note: 'settings.access.mcp.notePerplexity',
    node: true,
  },
  {
    id: 'qwen',
    name: 'Qwen Code',
    kind: 'terminal',
    group: 'local',
    mark: 'qwen',
    tile: 'glim-tile-qwen',
    setup: { kind: 'file', paths: home('.qwen/settings.json') },
    token: { kind: 'env', unsure: true },
  },
  {
    id: 'roo',
    name: 'Roo Code',
    kind: 'vscode',
    group: 'local',
    mark: 'roocode',
    tile: 'glim-tile-roocode',
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
    kind: 'editor',
    group: 'local',
    mark: 'visualstudio',
    tile: 'glim-tile-visualstudio',
    lift: true,
    setup: { kind: 'file', paths: { windows: '%USERPROFILE%\\.mcp.json' } },
    token: { kind: 'inFile' },
  },
  {
    id: 'warp',
    name: 'Warp',
    kind: 'terminal',
    group: 'local',
    mark: 'warp',
    tile: 'glim-tile-warp',
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
    kind: 'editor',
    group: 'local',
    mark: 'windsurf',
    tile: 'glim-tile-windsurf',
    setup: {
      kind: 'file',
      paths: { windows: '%APPDATA%\\devin\\mcp_config.json', unix: '~/.config/devin/mcp_config.json' },
    },
    token: { kind: 'env' },
  },
  {
    id: 'zed',
    name: 'Zed',
    kind: 'editor',
    group: 'local',
    mark: 'zed',
    tile: 'glim-tile-zed',
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
    kind: 'webChat',
    group: 'cloud',
    mark: 'grok',
    tile: 'glim-tile-grok',
    setup: { kind: 'form', ui: 'grok.com, Connectors, New Connector, Custom' },
    token: { kind: 'vendor' },
    note: 'settings.access.mcp.noteGrok',
  },
  {
    id: 'lechat',
    name: 'Le Chat',
    kind: 'webChat',
    group: 'cloud',
    mark: 'mistral',
    tile: 'glim-tile-mistral',
    setup: { kind: 'form', ui: 'Connectors, Add a Custom Connector' },
    token: { kind: 'vendor' },
  },
];

export const OTHER_CLIENT: McpClient = {
  id: 'other',
  name: '',
  group: 'local',
  tile: 'glim-tile-house',
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

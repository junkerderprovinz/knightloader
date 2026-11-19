// The MCP catalog: the AI apps that connect to an app's MCP server, each with
// its mark. An app passes the ids it can set up to catalogPicker and keeps what
// each setup needs (where the configuration lives, how the client comes by the
// key) beside them by id.
//
// A client stands by what people use it as: a chat app on the desktop, one that
// runs models of its own, a chat in the browser, an editor, an extension inside
// VS Code, a tool in the terminal, or a workflow that calls tools. The client
// that takes any address stands in the last group, and the app marks it last.
//
// A client wears its own mark or its maker's, never a neighbour's: a mark naming
// the wrong client is worse than the glyph of its kind.

import type { Catalog, CatalogEntry } from "./catalog";
import { MCP_MARKS as M } from "./mcpMarks";

export type McpGroup = "chat" | "models" | "web" | "editor" | "extension" | "terminal" | "automation";

const ENTRIES = [
  { id: "claude-desktop", name: "Claude Desktop", group: "chat", terms: ["Anthropic"], mark: M.claude },
  { id: "goose", name: "Goose", group: "chat", terms: ["Block"], mark: M.goose },
  { id: "perplexity", name: "Perplexity", group: "chat", mark: M.perplexity },

  { id: "anythingllm", name: "AnythingLLM", group: "models", mark: M.anythingllm },
  { id: "jan", name: "Jan", group: "models", glyph: "IconApp" },
  { id: "lmstudio", name: "LM Studio", group: "models", mark: M.lmstudio },
  { id: "msty", name: "Msty", group: "models", glyph: "IconApp" },
  { id: "openwebui", name: "Open WebUI", group: "models", terms: ["Ollama"], mark: M.openwebui },

  { id: "chatgpt", name: "ChatGPT", group: "web", terms: ["OpenAI"], mark: M.chatgpt },
  { id: "claude", name: "Claude", group: "web", terms: ["Anthropic", "claude.ai"], mark: M.claude },
  { id: "grok", name: "Grok", group: "web", terms: ["xAI"], mark: M.grok },
  { id: "lechat", name: "Le Chat", group: "web", terms: ["Mistral"], mark: M.mistral },

  { id: "antigravity", name: "Antigravity", group: "editor", terms: ["Google"], glyph: "IconCode" },
  { id: "cursor", name: "Cursor", group: "editor", mark: M.cursor },
  { id: "jetbrains", name: "JetBrains", group: "editor", terms: ["Junie", "AI Assistant", "IntelliJ IDEA"], mark: M.jetbrains },
  { id: "visualstudio", name: "Visual Studio", group: "editor", terms: ["Microsoft"], glyph: "IconCode" },
  { id: "windsurf", name: "Windsurf", group: "editor", terms: ["Codeium", "Devin"], mark: M.windsurf },
  { id: "zed", name: "Zed", group: "editor", mark: M.zed },

  { id: "cline", name: "Cline", group: "extension", mark: M.cline },
  { id: "continue", name: "Continue", group: "extension", mark: M.continuedev },
  { id: "copilot", name: "GitHub Copilot", group: "extension", terms: ["VS Code"], mark: M.copilot },
  { id: "roo", name: "Roo Code", group: "extension", mark: M.roo },

  { id: "claude-code", name: "Claude Code", group: "terminal", terms: ["Anthropic"], mark: M.claude },
  { id: "codex", name: "Codex CLI", group: "terminal", terms: ["OpenAI"], mark: M.openai },
  { id: "copilot-cli", name: "Copilot CLI", group: "terminal", terms: ["GitHub"], mark: M.copilot },
  { id: "gemini", name: "Gemini CLI", group: "terminal", terms: ["Google"], mark: M.gemini },
  { id: "kimi", name: "Kimi Code", group: "terminal", terms: ["Moonshot AI"], mark: M.kimi },
  { id: "opencode", name: "opencode", group: "terminal", mark: M.opencode },
  { id: "qwen", name: "Qwen Code", group: "terminal", terms: ["Alibaba"], mark: M.qwen },
  { id: "vibe", name: "Mistral Vibe", group: "terminal", mark: M.mistral },
  { id: "warp", name: "Warp", group: "terminal", mark: M.warp },

  { id: "n8n", name: "n8n", group: "automation", mark: M.n8n },
  { id: "other", name: "Other client", group: "automation", terms: ["MCP"], glyph: "IconLink", word: true },
] as const satisfies readonly CatalogEntry<McpGroup>[];

export type McpProviderId = (typeof ENTRIES)[number]["id"];

/** Every MCP client, with its group's place in the picker. */
export const MCP_PROVIDERS: Catalog<McpGroup, McpProviderId> = {
  groups: ["chat", "models", "web", "editor", "extension", "terminal", "automation"],
  entries: ENTRIES,
};

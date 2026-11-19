# The MCP clients' marks, and where they come from

`scripts/gen_mcp_marks.py` turns these files, and the marks it takes from
Simple Icons, into `src/mcpMarks.ts`: each mark as markup for `BrandMark`, with
the colour its tile lights up in ("Brand tiles" in the design language). The
MCP catalog in `src/mcpProviders.ts` names the mark each client wears, and an
app reaches them through `@glimstone/core/mcpProviders`.

The generator treats these files the way it treats the storage providers'
marks (see `../storage-marks/README.md`).

## Sources and licences

**Simple Icons** (<https://simpleicons.org>) for `claude`, `cline`, `cursor`,
`gemini` (`googlegemini`), `jetbrains`, `kimi`, `lmstudio`, `mistral`
(`mistralai`), `opencode`, `perplexity`, `qwen`, `warp`, `windsurf` and `zed`
(`zedindustries`). The set is **CC0 1.0** and asks for nothing.

**Dashboard Icons** (`homarr-labs/dashboard-icons`, <https://dashboardicons.com>),
under **Apache-2.0** (text in `LICENSE-apache-2.0.txt`), copyright the Homarr
Labs team and contributors: `chatgpt.svg`, `n8n.svg` and `openai.svg` are
the files of those names in `svg/`, `anythingllm.svg` is `svg/anything-llm.svg`
and `openwebui.svg` is `svg/open-webui.svg`. `grok.svg` is `svg/grok.svg`
without the id its two parts share, which an SVG may give only one element.
`copilot.svg` is `svg/github-copilot.svg`, which paints in `currentColor`, with
GitHub Copilot's black written on its drawing.

**The projects' own repositories**, each under **Apache-2.0**:

- `continuedev.svg`: `docs/logo/light.svg` from
  <https://github.com/continuedev/continue>, unmodified. Its id is not
  `continue`, which a script cannot name a constant.
- `goose.svg`: `documentation/static/img/goose.svg` from
  <https://github.com/block/goose>, unmodified.
- `roo.svg`: `src/assets/images/roo-logo.svg` from
  <https://github.com/RooCodeInc/Roo-Code>, which paints in `currentColor`,
  written in black.

Apache-2.0 may be carried into this project's AGPL-3.0, so the combined work
stays AGPL-3.0.

## Makers' marks for a narrower product

Claude Desktop, Claude and Claude Code wear Claude's mark. Codex CLI wears
OpenAI's, Gemini CLI Google Gemini's, Kimi Code Kimi's, Qwen Code Qwen's, Le
Chat and Mistral Vibe Mistral AI's, and Copilot CLI and GitHub Copilot the
Copilot mark.

A client whose own mark has no clean source wears the glyph of its kind
instead. Jan's logo exists only as a picture inside an SVG, in its own
repository as in Dashboard Icons, and Msty, Antigravity and Visual Studio have
none in an icon set or a repository of their own. Antigravity does not wear
Google's mark, which would name a neighbour on a page that also shows Gemini.

## Trademarks

Every mark here is a trademark of its owner. It is used the one way a trademark
may be used without permission, to refer to the thing it names: a mark sits on
the tile that picks **that client**, reproduced unmodified apart from its
colour, and nothing here claims endorsement by or affiliation with any of them.

## Adding one

For a mark Simple Icons carries, add a line to `SIMPLE` in the generator. For
any other, drop the SVG in here as its source has it, add a line to `LOCAL`
naming its source and licence, and record both in the section above. Then give
the client its `mark` in `src/mcpProviders.ts` and run the generator:

    npm pack simple-icons@16 && tar xzf simple-icons-*.tgz
    python scripts/gen_mcp_marks.py package/icons

It needs `svgelements` (`pip install svgelements`) to measure where a mark
paints.

# The MCP clients' marks, and where they come from

`scripts/gen_mcp_marks.py` turns these files, and the marks it takes from
Simple Icons, into `src/mcpMarks.ts`: each mark as markup for `BrandMark`, with
the colour its tile lights up in ("Brand tiles" in the design language). The
MCP catalog in `src/mcpProviders.ts` names the mark each client wears, and an
app reaches them through `@glimstone/core/mcpProviders`.

The generator treats these files the way it treats the storage providers'
marks (see `../storage-marks/README.md`). A logo of several colours that would
melt into one blot on its lit tile also brings `<id>-hover.svg`, the brand's
single-colour version, which the lit tile shows instead.

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
GitHub Copilot's black written on its drawing. `visualstudio.svg` is
`svg/visual-studio-2026.svg`, unmodified.

**Lobe Icons** (`lobehub/lobe-icons`, <https://github.com/lobehub/lobe-icons>,
published as `@lobehub/icons-static-svg`), under **MIT**, copyright LobeHub
(text in `LICENSE-mit-lobe-icons.txt`): `antigravity.svg` is
`icons/antigravity-color.svg` and `antigravity-hover.svg` is
`icons/antigravity.svg`, which paints in `currentColor`, with black written on
its drawing. Both leave out their width and height of `1em`, which only size
them to the text around them. The colour mark's blurred gradients would melt
into one blot on its lit tile, so the lit tile shows the single-colour arch.

**The projects' own repositories**, each under **Apache-2.0**:

- `continuedev.svg`: `docs/logo/light.svg` from
  <https://github.com/continuedev/continue>, unmodified. Its id is not
  `continue`, which a script cannot name a constant.
- `goose.svg`: `documentation/static/img/goose.svg` from
  <https://github.com/block/goose>, unmodified.
- `roo.svg`: `src/assets/images/roo-logo.svg` from
  <https://github.com/RooCodeInc/Roo-Code>, which paints in `currentColor`,
  written in black.

**The client's own website**, used under the trademark position below rather
than under a licence: `msty.svg` is <https://msty.ai/brands/studio.svg>, the
cube that <https://msty.ai/> names as its organisation's logo. Its tile lights
in the blue of the cube, `#198fe1`, rather than the black outline round it.

**The client's own picture, traced**, under the same trademark position:
`jan.svg` is the waving hand of <https://docs.jan.ai/assets/logo-jan.svg>, which
holds it only as a picture. It was traced from that 1024 px picture in its two
yellows and the grey of the motion lines. Both yellows fade into a light tile,
so the light theme deepens them as a pair, to `#a67c00` and `#8a6400`.

Apache-2.0 and MIT may be carried into this project's AGPL-3.0, so the combined
work stays AGPL-3.0.

## Makers' marks for a narrower product

Claude Desktop, Claude and Claude Code wear Claude's mark. Codex CLI wears
OpenAI's, Gemini CLI Google Gemini's, Kimi Code Kimi's, Qwen Code Qwen's, Le
Chat and Mistral Vibe Mistral AI's, and Copilot CLI and GitHub Copilot the
Copilot mark.

A client whose own mark has no clean source wears the glyph of its kind
instead. Only the catch-all tile for any other client does.

## Trademarks

Every mark here is a trademark of its owner. It is used the one way a trademark
may be used without permission, to refer to the thing it names: a mark sits on
the tile that picks **that client**, reproduced unmodified apart from its
colour, and nothing here claims endorsement by or affiliation with any of them.

## Adding one

For a mark Simple Icons carries, add a line to `SIMPLE` in the generator. For
any other, drop the SVG in here as its source has it, add a line to `LOCAL`
naming its source and licence, and record both in the section above. A logo
whose colours do not survive one ink gets its brand's single-colour version as
`<id>-hover.svg` and its id in `HOVER`. Then give
the client its `mark` in `src/mcpProviders.ts` and run the generator:

    npm pack simple-icons@16 && tar xzf simple-icons-*.tgz
    python scripts/gen_mcp_marks.py package/icons

It needs `svgelements` (`pip install svgelements`) to measure where a mark
paints.

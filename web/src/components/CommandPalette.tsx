// The command palette: every command visible right now, in one searchable
// overlay - build-plan.md's Wave-1D note ("Fix the command-record type … and
// the useCommands(surface, ctx) hook") and its Wave 12A row ("command
// registry + palette + rebindable shortcuts") name this file as the UI half
// of that plan. lib/commands/types.ts (Command, CommandSurface,
// CommandContext, useCommands, useCommandContext) is the registry core this
// file only reads from - it holds no command list of its own.
//
// Overlay mechanics follow ui.tsx's own Modal (see CaptchaModal.tsx, its
// only real-world caller): a dimmed backdrop, Escape and an outside click
// both close it, mounted once in Layout.tsx beside CaptchaModal,
// IdleActionBanner and OnboardingWizard rather than per page - a command
// has nothing to do with which route happens to be open when it is invoked.
// It is not built on top of <Modal> directly: that component's title-plus-
// footer shape is for a decision with a primary action, and this is a
// search field over a list, closer to LanguagePicker.tsx's own dropdown or
// ContextMenu.tsx's own keyboard walking than to a dialog - so the parts
// reused are the mechanics (backdrop, Escape, glim-card), not the component.
//
// Open state lives in lib/commandPaletteOpen.ts, not a local useState: a
// command's own `run` (lib/commands/global.ts's "open command palette"
// entry) has no reference to this component to call a prop on, so the two
// meet through the same module-scope store LanguagePicker.tsx and
// lib/langPickerOpen.ts already use for the identical reason.
import { useEffect, useMemo, useRef, useState } from 'react';
import { useLocation } from 'react-router-dom';
import { useCommandContext, useCommands, type Command, type CommandSurface } from '../lib/commands/types';
import { formatShortcut } from '../lib/commands/shortcuts';
import { setCommandPaletteOpen, useCommandPaletteOpen } from '../lib/commandPaletteOpen';
import { useT, type TranslationKey } from '../lib/i18n';
import { IconSearch } from '../lib/icons';

/** The route's first segment, the same split Layout.tsx already keys its enter animation on, mapped to the surface it corresponds to. */
const SECTION_SURFACE: Record<string, CommandSurface> = {
  downloads: 'downloads',
  collector: 'collector',
  instances: 'instances',
  accounts: 'accounts',
  settings: 'settings',
};

/**
 * score ranks a label against a query with no fuzzy-search dependency (this
 * app has none, and one small scorer does not justify adding one): a
 * literal substring match wins, ranked by how early it starts and whether it
 * starts on a word boundary, so "down" ranks "Downloads" above "Slow down".
 * Failing that, a subsequence match - every character of the query appears
 * in the label, in order, not necessarily together - still counts, ranked
 * behind every substring hit, so "cmdp" still finds "Command palette".
 * -1 means "does not match at all".
 */
function score(label: string, query: string): number {
  if (!query) return 0;
  const l = label.toLowerCase();
  const q = query.toLowerCase();
  const i = l.indexOf(q);
  if (i === 0) return 0;
  if (i > 0) return l[i - 1] === ' ' ? 1 : 2 + i;
  let cursor = 0;
  for (const ch of q) {
    cursor = l.indexOf(ch, cursor);
    if (cursor === -1) return -1;
    cursor++;
  }
  return 1000;
}

/**
 * groupLabel resolves a command's `group` to display text.
 *
 * `group` is documented (lib/commands/types.ts, lib/locales/en.ts's own
 * "commands.group.*" block) as a real TranslationKey string, e.g.
 * "commands.group.navigation" - never literal English - so this looks it up
 * exactly the way any other label on screen is. A command whose author has
 * not yet followed that convention (a bare word, or a key not landed in
 * en.ts yet) falls back to showing that raw string rather than a blank
 * header - the same "never a blank label, worst case the raw token" rule
 * columns.tsx's reasonKey already follows for an open, multi-author
 * vocabulary.
 */
function groupLabel(t: (k: TranslationKey) => string, group: string): string {
  return t(group as TranslationKey) || group;
}

export function CommandPalette() {
  const { t } = useT();
  const location = useLocation();
  const open = useCommandPaletteOpen();
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const itemRefs = useRef(new Map<string, HTMLButtonElement>());

  const section = location.pathname.split('/')[1] ?? '';
  const surface: CommandSurface = SECTION_SURFACE[section] ?? 'overview';
  const ctx = useCommandContext(surface);
  const commands = useCommands(surface, ctx);

  // No bootstrap listener of its own: CommandDispatcher.tsx (mounted
  // alongside this component in app/Layout.tsx, never one without the
  // other) already matches every command's EFFECTIVE shortcut - including
  // "open command palette" itself, a 'global'-surfaced Command like any
  // other - against every keystroke, live overrides included, and calls
  // run(), which sets the same flag this component reads. A second listener
  // here duplicated that match against the command's raw, never-updated
  // defaultShortcut: rebinding "open command palette" in Settings >
  // Shortcuts would retire the old mod+k for the dispatcher but not for
  // this component, which kept answering to it forever, and if the freed
  // mod+k was then rebound onto a DIFFERENT command, one keystroke fired
  // both. Removed rather than fixed in place - CommandDispatcher is already
  // the one general mechanism this needs, and a second implementation of
  // the same match is exactly the kind of duplication that drifts again.

  // Fresh search state every time it opens - a stale query or selection
  // from the last time it was open must never be what somebody sees first.
  // Focus goes back to whatever had it before, the same restore
  // ContextMenu.tsx's own outermost panel already does.
  useEffect(() => {
    if (!open) return;
    setQuery('');
    setActive(0);
    const opener = document.activeElement as HTMLElement | null;
    const raf = requestAnimationFrame(() => inputRef.current?.focus());
    return () => {
      cancelAnimationFrame(raf);
      opener?.focus?.();
    };
  }, [open]);

  const filtered = useMemo(() => {
    const scored = commands.map((c) => ({ c, s: score(t(c.labelKey), query) })).filter((x) => x.s >= 0);
    // Stable sort: ties (score 0 with no query at all) keep useCommands' own
    // group-then-id order, so the palette's default view is not scrambled.
    scored.sort((a, b) => a.s - b.s);
    return scored.map((x) => x.c);
  }, [commands, query, t]);

  useEffect(() => {
    setActive((i) => Math.min(i, Math.max(0, filtered.length - 1)));
  }, [filtered.length]);

  useEffect(() => {
    const cmd = filtered[active];
    if (cmd) itemRefs.current.get(cmd.id)?.scrollIntoView({ block: 'nearest' });
  }, [active, filtered]);

  const groups = useMemo(() => {
    const byGroup = new Map<string, Command[]>();
    for (const c of filtered) {
      const list = byGroup.get(c.group);
      if (list) list.push(c);
      else byGroup.set(c.group, [c]);
    }
    return [...byGroup.entries()];
  }, [filtered]);

  function runIfEnabled(cmd: Command): void {
    if (!cmd.enabled(ctx)) return;
    setCommandPaletteOpen(false);
    void cmd.run(ctx);
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      setCommandPaletteOpen(false);
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActive((i) => Math.min(i + 1, filtered.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActive((i) => Math.max(i - 1, 0));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const cmd = filtered[active];
      if (cmd) runIfEnabled(cmd);
    } else if (e.key === 'Tab') {
      // Tab out of the palette means "I am done with it" - the same rule
      // ContextMenu.tsx's own Panel already follows for the identical key.
      e.preventDefault();
      setCommandPaletteOpen(false);
    }
  }

  if (!open) return null;

  let rowIndex = -1;

  return (
    <div
      className="fixed inset-0 z-[70] flex justify-center bg-black/50 px-4 pt-[14vh]"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) setCommandPaletteOpen(false);
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t('commands.paletteLabel')}
        onKeyDown={onKeyDown}
        className="glim-card glim-fade flex h-fit max-h-[70vh] w-full max-w-lg flex-col overflow-hidden"
      >
        <div className="flex items-center gap-2.5 border-b border-carbon-border/60 px-4 py-3">
          <IconSearch width={16} height={16} className="shrink-0 text-carbon-textMuted" />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('commands.searchPlaceholder')}
            aria-label={t('commands.searchPlaceholder')}
            role="combobox"
            aria-expanded="true"
            aria-controls="command-palette-list"
            aria-activedescendant={filtered[active] ? `command-palette-opt-${filtered[active].id}` : undefined}
            className="min-w-0 flex-1 bg-transparent text-sm text-carbon-text placeholder:text-carbon-textMuted outline-none"
          />
        </div>

        <div id="command-palette-list" role="listbox" aria-label={t('commands.paletteLabel')} className="overflow-y-auto py-1.5">
          {filtered.length === 0 && (
            <p className="px-4 py-6 text-center text-xs text-carbon-textMuted">{t('commands.noResults')}</p>
          )}
          {groups.map(([group, cmds]) => (
            <div key={group} className="py-1">
              <div className="glim-eyebrow px-4 pb-1">{groupLabel(t, group)}</div>
              {cmds.map((cmd) => {
                rowIndex++;
                const i = rowIndex;
                const isActive = i === active;
                const disabled = !cmd.enabled(ctx);
                const Icon = cmd.icon;
                return (
                  <button
                    key={cmd.id}
                    id={`command-palette-opt-${cmd.id}`}
                    role="option"
                    aria-selected={isActive}
                    aria-disabled={disabled}
                    type="button"
                    ref={(el) => {
                      if (el) itemRefs.current.set(cmd.id, el);
                      else itemRefs.current.delete(cmd.id);
                    }}
                    disabled={disabled}
                    onMouseEnter={() => setActive(i)}
                    onClick={() => runIfEnabled(cmd)}
                    className={`flex w-full items-center gap-2.5 px-4 py-2 text-start text-sm transition-colors
                      outline-none disabled:pointer-events-none disabled:opacity-35 ${
                        isActive ? 'bg-carbon-hover text-carbon-text' : 'text-carbon-textSub'
                      }`}
                  >
                    {Icon && <Icon className="h-4 w-4 shrink-0 text-carbon-textMuted" />}
                    <span className="min-w-0 flex-1 truncate">{t(cmd.labelKey)}</span>
                    {cmd.defaultShortcut && (
                      <span className="glim-num shrink-0 text-[11px] text-carbon-textMuted">
                        {formatShortcut(cmd.defaultShortcut, t)}
                      </span>
                    )}
                  </button>
                );
              })}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

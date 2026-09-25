// The command palette: every command available on the current surface in one
// searchable overlay, reading from the registry in lib/commands/types.ts. It
// is a search over a list rather than a decision, so it borrows Modal's
// mechanics but not the component; it is still a window, with the title badge
// and the bottom row that go with one. The open state lives in
// lib/commandPaletteOpen.ts so a command's `run` can open it.
import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { useLocation } from 'react-router-dom';
import { useCommandContext, useCommands, type Command, type CommandSurface } from '../lib/commands/types';
import { formatShortcut } from '../lib/commands/shortcuts';
import { setCommandPaletteOpen, useCommandPaletteOpen } from '../lib/commandPaletteOpen';
import { useT, type TranslationKey } from '../lib/i18n';
import { IconClose, IconSearch } from '../lib/icons';
import { Button, SectionTitle } from './ui';
// Shared with the settings search, so both rank a word the same way.
import { score } from '../lib/rank';
import { openWindow } from '../lib/windowStack';

/** The route's first segment mapped to its surface, as in Layout.tsx. */
const SECTION_SURFACE: Record<string, CommandSurface> = {
  downloads: 'downloads',
  collector: 'collector',
  instances: 'instances',
  accounts: 'accounts',
  settings: 'settings',
};

// groupLabel translates a command's `group` key, falling back to the raw string
// rather than a blank header when the key is missing.
function groupLabel(t: (k: TranslationKey) => string, group: string): string {
  return t(group as TranslationKey) || group;
}

/**
 * CommandPalette is mounted once in Layout.tsx. It has no key listener of its
 * own: CommandDispatcher matches the effective shortcut that opens it.
 */
export function CommandPalette() {
  const { t } = useT();
  const location = useLocation();
  const open = useCommandPaletteOpen();
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const itemRefs = useRef(new Map<string, HTMLButtonElement>());
  const backdrop = useRef<HTMLDivElement>(null);
  const titleId = useId();

  const section = location.pathname.split('/')[1] ?? '';
  const surface: CommandSurface = SECTION_SURFACE[section] ?? 'overview';
  const ctx = useCommandContext(surface);
  const commands = useCommands(surface, ctx);

  // Fresh search state on every open, and focus returns to the opener on close.
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

  // On the window stack like every window, so a captcha that arrives while the
  // palette is open leaves the focus in its search box.
  useEffect(() => {
    const el = backdrop.current;
    if (!open || !el) return;
    return openWindow(el, () => setCommandPaletteOpen(false));
  }, [open]);

  const filtered = useMemo(() => {
    const scored = commands.map((c) => ({ c, s: score(t(c.labelKey), query) })).filter((x) => x.s >= 0);
    // A stable sort keeps useCommands' order for ties, such as an empty query.
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
      // Kept from the stack's listener, which would otherwise also close a
      // window portalled in after the palette, under it.
      e.preventDefault();
      e.stopPropagation();
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
      // Tab closes, as in ContextMenu.
      e.preventDefault();
      setCommandPaletteOpen(false);
    }
  }

  if (!open) return null;

  let rowIndex = -1;

  return (
    <div
      ref={backdrop}
      className="glim-modal-backdrop fixed inset-0 z-[70] flex justify-center px-4 pt-[14vh]"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) setCommandPaletteOpen(false);
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        onKeyDown={onKeyDown}
        // glim-modal-card alone: it already fades, and glim-fade would run a
        // second arrival with its own duration. No overflow-hidden, which
        // would clip the title badge where it overhangs the top edge; the
        // list scrolls on its own.
        className="glim-card glim-modal-card flex h-fit max-h-[70vh] w-full max-w-lg flex-col pt-3"
      >
        {/* On the rows' own inset, so the badge lines up with the search glyph. */}
        <div className="px-4">
          <SectionTitle id={titleId}>{t('commands.paletteLabel')}</SectionTitle>
        </div>
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

        {/* The way out, for the pointer; the keyboard has Escape and Tab. */}
        <div className="flex items-center justify-end gap-3 px-4 pb-4 pt-2">
          <Button
            kind="ghost"
            labelled
            icon={<IconClose />}
            title={t('common.close')}
            onClick={() => setCommandPaletteOpen(false)}
          />
        </div>
      </div>
    </div>
  );
}

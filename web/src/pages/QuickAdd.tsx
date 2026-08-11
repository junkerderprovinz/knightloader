import { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { addLinksWithOptions, remove, type Task } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { Button, Card, Field, TextArea } from '../components/ui';
import { IconDownloads } from '../lib/icons';

/**
 * The one page a bookmarklet click, an extension action, and a PWA share all
 * land on — see lib/browserTools.ts's own doc comment for why all three open
 * this address rather than calling the API some other way. Deliberately
 * outside <Layout> (see app/router.tsx): the bookmarklet opens this in a
 * small window sized for exactly this content, and a sidebar squeezed into
 * 420px is worse than no sidebar.
 *
 * AuthGate (app/AuthGate.tsx) still wraps it like every other route, so a
 * password-locked instance shows the normal sign-in first — on the SAME
 * URL, because AuthGate only decides what to render, never navigates, so
 * the query string is exactly as intact after signing in as before it.
 *
 * The strings this page needs are not in en.ts yet - locale files are one
 * writer's lane per wave (11G, phase 3 of this one), same arrangement
 * System.tsx, Diagnostics.tsx, Schedule.tsx and Captcha.tsx already use.
 */
const PENDING = {
  'quickadd.title': 'Add to KnightLoader',
  'quickadd.manualLabel': 'Link (or paste several, one per line)',
  'quickadd.manualPlaceholder': 'https://example.com/file.zip',
  'quickadd.add': 'Add',
  'quickadd.adding': 'Adding…',
  'quickadd.emptyHint': 'Nothing was shared — paste a link by hand, or use this page from the bookmarklet, the browser extension, or your device’s Share menu.',
  'quickadd.staged': 'Added to the collector.',
  'quickadd.stagedNamed': 'Added “{name}” to the collector.',
  'quickadd.stagedCount': 'Added {n} links to the collector.',
  'quickadd.none': 'Nothing was added — every link here was already in the collector.',
  'quickadd.failed': 'Could not add this: {error}',
  'quickadd.undo': 'Undo',
  'quickadd.undone': 'Removed.',
  'quickadd.openCollector': 'Open Collector',
  'quickadd.close': 'Close window',
} as const;

type PendingKey = keyof typeof PENDING;

function useCx() {
  const { t } = useT();
  return useCallback(
    (key: PendingKey, vars?: Record<string, string | number>) => {
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? PENDING[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [t],
  );
}

type Phase = { kind: 'form' } | { kind: 'busy' } | { kind: 'done'; created: Task[] } | { kind: 'error'; message: string } | { kind: 'undone' };

export function QuickAdd() {
  const cx = useCx();
  const [params] = useSearchParams();
  const url = params.get('url') ?? '';
  const text = params.get('text') ?? '';
  const title = params.get('title') ?? '';
  // A share can carry both a url and prose that mentions one; joined rather
  // than picking one, because linkscan on the server (internal/linkscan,
  // reused by POST /api/links) already extracts every URL out of a blob and
  // silently keeps only what looks like a link either way. Joined with a
  // BLANK line, not a single newline: linkscan's own hard-wrap rejoin
  // (logicalLines/continuesURL) glues a line ending in a URL to whatever
  // follows it when that next line starts lowercase - real, reproduced with
  // the actual Android share-sheet shape (both url and text set), which
  // turned "https://example.com/file.zip" + "some text" into a single
  // corrupted "https://example.com/file.zipsome" link. A blank line between
  // them is what continuesURL's own empty-string guard refuses to bridge.
  const shared = [url, text].filter(Boolean).join('\n\n');

  const [phase, setPhase] = useState<Phase>({ kind: shared ? 'busy' : 'form' });
  const [manual, setManual] = useState('');
  const isPopup = typeof window !== 'undefined' && !!window.opener;

  const stage = useCallback(
    async (blob: string) => {
      setPhase({ kind: 'busy' });
      try {
        const created = await addLinksWithOptions(blob, { package: title || undefined });
        setPhase(created.length ? { kind: 'done', created } : { kind: 'error', message: cx('quickadd.none') });
      } catch (e) {
        setPhase({ kind: 'error', message: cx('quickadd.failed', { error: String(e).replace(/^Error:\s*/, '') }) });
      }
    },
    [title, cx],
  );

  // Auto-submits once, on the params the page was opened with — the whole
  // point of a bookmarklet is one click total, not a click to open this page
  // and a second one to confirm what it already knows.
  useEffect(() => {
    if (shared) void stage(shared);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function undo(created: Task[]) {
    await Promise.all(created.map((t) => remove(t.id)));
    setPhase({ kind: 'undone' });
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-carbon-background p-6">
      <div className="flex w-full max-w-sm flex-col gap-4">
        <div className="flex items-center gap-2">
          <IconDownloads width={20} height={20} className="text-accent" />
          <span className="text-[15px] font-semibold text-carbon-text">{cx('quickadd.title')}</span>
        </div>

        <Card className="flex flex-col gap-4">
          {phase.kind === 'form' && (
            <>
              <p className="text-xs text-carbon-textMuted">{cx('quickadd.emptyHint')}</p>
              <Field label={cx('quickadd.manualLabel')}>
                <TextArea
                  rows={4}
                  autoFocus
                  value={manual}
                  placeholder={cx('quickadd.manualPlaceholder')}
                  onChange={(e) => setManual(e.target.value)}
                />
              </Field>
              <Button disabled={manual.trim() === ''} onClick={() => void stage(manual)}>
                {cx('quickadd.add')}
              </Button>
            </>
          )}

          {phase.kind === 'busy' && <p className="text-sm text-carbon-textSub">{cx('quickadd.adding')}</p>}

          {phase.kind === 'done' && (
            <>
              <p className="text-sm text-statusOk">
                {phase.created.length === 1
                  ? phase.created[0].name
                    ? cx('quickadd.stagedNamed', { name: phase.created[0].name })
                    : cx('quickadd.staged')
                  : cx('quickadd.stagedCount', { n: phase.created.length })}
              </p>
              <div className="flex flex-wrap items-center gap-3">
                <Button kind="ghost" className="px-2.5 text-xs" onClick={() => void undo(phase.created)}>
                  {cx('quickadd.undo')}
                </Button>
                {isPopup ? (
                  <Button kind="secondary" className="px-2.5 text-xs" onClick={() => window.close()}>
                    {cx('quickadd.close')}
                  </Button>
                ) : (
                  <a href="/collector" className="text-xs text-accent hover:underline">
                    {cx('quickadd.openCollector')}
                  </a>
                )}
              </div>
            </>
          )}

          {phase.kind === 'undone' && <p className="text-sm text-carbon-textSub">{cx('quickadd.undone')}</p>}

          {phase.kind === 'error' && (
            <>
              <p className="text-sm text-statusFail">{phase.message}</p>
              <Button kind="secondary" onClick={() => setPhase({ kind: 'form' })}>
                {cx('quickadd.add')}
              </Button>
            </>
          )}
        </Card>
      </div>
    </div>
  );
}

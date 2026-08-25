import { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { addLinksWithOptions, remove, type Task } from '../lib/api';
import { useT } from '../lib/i18n';
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
 * Every string here lives in lib/locales like any other page. It used to
 * carry its own PENDING fallback table, from the wave that added this page
 * before the locale files caught up - long since redundant, since all but two
 * of its entries had already been superseded by the real catalogue and the
 * fallback only ever shadowed a translation that existed.
 */

type Phase = { kind: 'form' } | { kind: 'busy' } | { kind: 'done'; created: Task[] } | { kind: 'error'; message: string } | { kind: 'undone' };

export function QuickAdd() {
  const { t } = useT();
  const [params] = useSearchParams();
  const url = params.get('url') ?? '';
  const text = params.get('text') ?? '';
  const title = params.get('title') ?? '';
  // Issue #27: which instance this link is FOR. Empty means this one, which
  // is every existing caller - the bookmarklet, the share target, and an
  // extension entry that has an address of its own.
  //
  // It exists for the peers that have no address at all: a desktop build, or
  // anything reachable only through a relay. Those cannot be opened in a tab,
  // so the extension cannot send them anything directly - but THIS instance
  // is already federated with them, over whichever transport works, and
  // /api/instances/{name}/links is a route it already forwards
  // (routes_federation.go's own allowlist). Routing through the one instance
  // the browser CAN reach is what makes those peers reachable, and it needs
  // no relay client, no second copy of the relay key, and no persistent
  // socket in a service worker that the browser is free to kill.
  const to = params.get('to') ?? '';
  const apiBase = to ? `/api/instances/${encodeURIComponent(to)}` : '/api';
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
        const created = await addLinksWithOptions(blob, { package: title || undefined }, apiBase);
        setPhase(created.length ? { kind: 'done', created } : { kind: 'error', message: t('quickadd.none') });
      } catch (e) {
        setPhase({ kind: 'error', message: t('quickadd.failed', { error: String(e).replace(/^Error:\s*/, '') }) });
      }
    },
    [title, t, apiBase],
  );

  // Auto-submits once, on the params the page was opened with — the whole
  // point of a bookmarklet is one click total, not a click to open this page
  // and a second one to confirm what it already knows.
  useEffect(() => {
    if (shared) void stage(shared);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function undo(created: Task[]) {
    // Checked, not fired and forgotten. `remove` is a bare fetch, which
    // resolves happily on a 502 - so the old version reported "Removed." for a
    // delete that did not happen. That was survivable while every undo was a
    // same-process call; with `?to=` it crosses to another instance, where a
    // peer that went offline between the add and the undo is an ordinary
    // Tuesday, and the link stays queued on a machine the user believes they
    // just cleared.
    try {
      const results = await Promise.all(created.map((t) => remove(t.id, apiBase)));
      const failed = results.filter((r) => !r.ok);
      if (failed.length > 0) {
        setPhase({ kind: 'error', message: t('quickadd.undoFailed', { error: String(failed[0].status) }) });
        return;
      }
      setPhase({ kind: 'undone' });
    } catch (e) {
      setPhase({ kind: 'error', message: t('quickadd.undoFailed', { error: String(e).replace(/^Error:\s*/, '') }) });
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-carbon-background p-6">
      <div className="flex w-full max-w-sm flex-col gap-4">
        <div className="flex items-center gap-2">
          <IconDownloads width={20} height={20} className="text-accent" />
          <span className="text-[15px] font-semibold text-carbon-text">{t('quickadd.title')}</span>
        </div>

        {/* Named, always, when this is not the instance being looked at: a
            link quietly landing on a different machine is the one thing this
            page must never do. */}
        {to !== '' && <p className="-mt-2 text-xs text-carbon-textMuted">{t('quickadd.toPeer', { name: to })}</p>}

        <Card className="flex flex-col gap-4">
          {phase.kind === 'form' && (
            <>
              <p className="text-xs text-carbon-textMuted">{t('quickadd.emptyHint')}</p>
              <Field label={t('quickadd.manualLabel')}>
                <TextArea
                  rows={4}
                  autoFocus
                  value={manual}
                  placeholder={t('quickadd.manualPlaceholder')}
                  onChange={(e) => setManual(e.target.value)}
                />
              </Field>
              <Button disabled={manual.trim() === ''} onClick={() => void stage(manual)}>
                {t('quickadd.add')}
              </Button>
            </>
          )}

          {phase.kind === 'busy' && <p className="text-sm text-carbon-textSub">{t('quickadd.adding')}</p>}

          {phase.kind === 'done' && (
            <>
              <p className="text-sm text-statusOk">
                {phase.created.length === 1
                  ? phase.created[0].name
                    ? t('quickadd.stagedNamed', { name: phase.created[0].name })
                    : t('quickadd.staged')
                  : t('quickadd.stagedCount', { n: phase.created.length })}
              </p>
              <div className="flex flex-wrap items-center gap-3">
                <Button kind="ghost" className="px-2.5 text-xs" onClick={() => void undo(phase.created)}>
                  {t('quickadd.undo')}
                </Button>
                {isPopup ? (
                  <Button kind="secondary" className="px-2.5 text-xs" onClick={() => window.close()}>
                    {t('quickadd.close')}
                  </Button>
                ) : (
                  <a href="/collector" className="text-xs text-accent hover:underline">
                    {t('quickadd.openCollector')}
                  </a>
                )}
              </div>
            </>
          )}

          {phase.kind === 'undone' && <p className="text-sm text-carbon-textSub">{t('quickadd.undone')}</p>}

          {phase.kind === 'error' && (
            <>
              <p className="text-sm text-statusFail">{phase.message}</p>
              <Button kind="secondary" onClick={() => setPhase({ kind: 'form' })}>
                {t('quickadd.add')}
              </Button>
            </>
          )}
        </Card>
      </div>
    </div>
  );
}

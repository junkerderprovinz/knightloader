import { useCallback, useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { addLinksWithOptions, remove, type Task } from '../lib/api';
import { useT } from '../lib/i18n';
import { Button, Card, Field, TextArea } from '../components/ui';
import { IconDownloads } from '../lib/icons';

/**
 * QuickAdd is the page a bookmarklet, the extension and a PWA share all open
 * (lib/browserTools.ts). It sits outside <Layout> because the bookmarklet opens
 * it in a small window. AuthGate never navigates, so the query string survives
 * signing in.
 */

type Phase = { kind: 'form' } | { kind: 'busy' } | { kind: 'done'; created: Task[] } | { kind: 'error'; message: string } | { kind: 'undone' };

export function QuickAdd() {
  const { t } = useT();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const url = params.get('url') ?? '';
  const text = params.get('text') ?? '';
  const title = params.get('title') ?? '';
  // The peer the link is for, empty for this instance. It reaches peers the
  // browser cannot open, such as a desktop build or a relay-only instance,
  // through this one's federation (routes_federation.go).
  const to = params.get('to') ?? '';
  const apiBase = to ? `/api/instances/${encodeURIComponent(to)}` : '/api';
  // A share can carry a url and text; linkscan extracts the links from both.
  // A blank line keeps its hard-wrap rejoin (continuesURL) from gluing the url
  // to a following line that starts lowercase.
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

  // Submits once on open, so a bookmarklet takes one click.
  useEffect(() => {
    if (shared) void stage(shared);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function undo(created: Task[]) {
    // `remove` resolves on a 502 too, and with `?to=` the peer may have gone
    // offline, so every result is checked.
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
          <IconDownloads width={20} height={20} className="text-accentInk" />
          <span className="text-xl font-semibold text-carbon-text">{t('quickadd.title')}</span>
        </div>

        {/* A link bound for another machine always names it. */}
        {to !== '' && <p className="-mt-2 text-xs text-carbon-textMuted">{t('quickadd.toPeer', { name: to })}</p>}

        <Card className="flex flex-col gap-4">
          {phase.kind === 'form' && (
            <>
              <Field label={t('quickadd.manualLabel')} hint={t('quickadd.manualHint')}>
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
                  <Button kind="secondary" className="px-2.5 text-xs" onClick={() => navigate('/collector')}>
                    {t('quickadd.openCollector')}
                  </Button>
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

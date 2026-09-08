import { useEffect, useState } from 'react';
import { Button, Card, FieldGroup, IconBadge, SectionTitle, ToggleRow } from '../../../components/ui';
import { CookieJarDialog } from '../../../components/CookieJarDialog';
import { IconPlus, IconTrash } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import { fetchYtdlpCookieHosts, removeYtdlpCookieJar } from '../../../lib/api';
import { useDraft } from '../context';

/**
 * The stored cookies.txt files, one per site, and the switch that arms them.
 *
 * THREE PROPERTIES THIS CARD EXISTS TO KEEP, and each of them decided something
 * about the shape:
 *
 * A JAR IS NEVER SHOWN AGAIN. It is a live sign-in session, so it travels one
 * way only, into this server. That is why there is no "edit" that fills a box
 * back in, and why the list is host names and a marker word. Replacing a jar
 * means pasting the new file over it, which reads as a limitation and is the
 * feature: a page that could show a cookies.txt would be a page that hands one
 * to anybody who reaches this instance.
 *
 * THE SERVER OWNS THE KEY. It lower-cases the host, strips a leading "www." and
 * reduces a whole pasted address to its host, exactly the way every lookup keys
 * them, so what comes back can differ from what was typed. The list is
 * therefore always the server's answer and never the request: a jar typed as
 * "www.youtube.com" and drawn as typed would look stored under a name that is
 * never consulted.
 *
 * REMOVING IS NOT FREE. The server answers 404 for a host with nothing stored,
 * and that refusal is shown rather than swallowed. A green tick on a typo would
 * leave somebody believing a session is gone from this machine while it is
 * still sealed under the name they meant to type.
 *
 * THE PASTING ITSELF IS NOT IN HERE. It is CookieJarDialog, the same window a
 * failed download opens from its own "store cookies for this site" button. One
 * form, opened from the two places somebody arrives at this problem from -
 * rather than a settings form and a dialog form that agree today and drift the
 * first time either learns something. This card passes armSwitch={false},
 * because the switch for that very setting is on this card already and two
 * controls for one setting on one screen is how a page disagrees with itself.
 *
 * Everything under the switch dims while it is off. A jar stored for a feature
 * that is switched off does nothing at all, and that is a thing somebody would
 * do once and then spend an evening on.
 */
export function CookieJarsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const [hosts, setHosts] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [adding, setAdding] = useState(false);

  const enabled = cfg.ytdlp.cookies;

  useEffect(() => {
    let alive = true;
    void fetchYtdlpCookieHosts().then(
      (list) => {
        if (alive) setHosts(list);
      },
      () => {
        /* An empty table rather than a claim that nothing is stored: adding
           still works, and a save answers with the real list. */
      },
    );
    return () => {
      alive = false;
    };
  }, []);

  const run = async (work: () => Promise<string[]>) => {
    setBusy(true);
    setError('');
    try {
      setHosts(await work());
    } catch (e) {
      // The server's own sentence, which names the host and says what happened.
      // A key of ours here would be a second, vaguer copy of it.
      setError(String(e).replace(/^(Error|ApiError):\s*/, ''));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{t('settings.resolvers.cookiesTitle')}</SectionTitle>

      {/* The one control on this card that rides the shared Save bar: it is a
          settings field like every other one on this page. The jars themselves
          are not - they live behind their own routes and never touch the
          settings document, precisely so a sign-in session cannot end up in
          something that is read back, diffed and echoed into a diagnostics
          bundle. */}
      <ToggleRow
        checked={enabled}
        onChange={(v) => patch({ ytdlp: { ...cfg.ytdlp, cookies: v } })}
        label={t('settings.resolvers.cookies')}
        hint={t('settings.resolvers.cookiesHint')}
      />

      <div className={`flex flex-col gap-4 ${enabled ? '' : 'pointer-events-none opacity-40'}`}>
        <FieldGroup label={t('settings.resolvers.cookieJars')} hint={t('settings.resolvers.cookieJarsHint')}>
          {/* The glim-well wrapper with a plain list inside, as the accounts
              table does it - never a nested Card, and never a Card per row. */}
          <div className="glim-well p-0">
            {hosts.length === 0 ? (
              <p className="px-4 py-3 text-sm text-carbon-textMuted">{t('settings.resolvers.cookieEmpty')}</p>
            ) : (
              <ul className="flex flex-col">
                {hosts.map((h, i) => (
                  <li
                    key={h}
                    className={`group flex items-center gap-3 px-4 py-2.5 ${
                      i === hosts.length - 1 ? '' : 'border-b border-carbon-border/60'
                    }`}
                  >
                    <span className="text-sm text-carbon-text" dir="ltr">
                      {h}
                    </span>
                    <span className="glim-eyebrow">{t('settings.resolvers.cookieStored')}</span>
                    {/* Revealed on hover or on keyboard focus, the same
                        treatment the host-rules and preset tables give the
                        deletion of a per-host row, so a long list reads as
                        content rather than as a column of red buttons. */}
                    <IconBadge
                      className="ms-auto opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100"
                      icon={<IconTrash width={16} height={16} />}
                      title={`${t('settings.resolvers.cookieRemove')} · ${h}`}
                      aria-label={`${t('settings.resolvers.cookieRemove')} · ${h}`}
                      disabled={busy || !enabled}
                      onClick={() => void run(() => removeYtdlpCookieJar(h))}
                    />
                  </li>
                ))}
              </ul>
            )}
          </div>
        </FieldGroup>

        <div className="flex items-center gap-3">
          <Button
            kind="secondary"
            icon={<IconPlus width={16} height={16} />}
            disabled={busy || !enabled}
            onClick={() => {
              setError('');
              setAdding(true);
            }}
          >
            {t('cookies.add')}
          </Button>
          {error && <p className="text-xs text-statusWarn">{error}</p>}
        </div>
      </div>

      {/* Outside the dimmed block above: an overlay that inherited its
          pointer-events-none would be a window nobody could type into. */}
      {adding && (
        <CookieJarDialog
          armSwitch={false}
          onClose={() => setAdding(false)}
          onSaved={(list) => setHosts(list)}
        />
      )}
    </Card>
  );
}

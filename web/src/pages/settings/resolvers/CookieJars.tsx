import { useEffect, useState } from 'react';
import { Button, Card, FieldGroup, IconBadge, SectionTitle, ToggleRow } from '../../../components/ui';
import { CookieJarDialog } from '../../../components/CookieJarDialog';
import { IconPlus, IconTrash } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import { fetchYtdlpCookieHosts, removeYtdlpCookieJar } from '../../../lib/api';
import { useDraft } from '../context';

/**
 * CookieJarsCard lists the stored cookies.txt files, one per site, beside the
 * switch that lets yt-dlp use them. A jar is a live session and is never shown
 * again, so the list holds only the host names the server keyed them under.
 * Pasting happens in CookieJarDialog, shared with the failed-download button,
 * with armSwitch={false} because the switch is on this card.
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
        /* Adding still works, and a save answers with the real list. */
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
      // The server's sentence names the host, including the 404 for a typo.
      setError(String(e).replace(/^(Error|ApiError):\s*/, ''));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{t('settings.resolvers.cookiesTitle')}</SectionTitle>

      {/* The switch is a settings field; the jars have their own routes so a
          session never enters the settings document. */}
      <ToggleRow
        checked={enabled}
        onChange={(v) => patch({ ytdlp: { ...cfg.ytdlp, cookies: v } })}
        label={t('settings.resolvers.cookies')}
        hint={t('settings.resolvers.cookiesHint')}
      />

      {/* The jars stay usable while the switch is off: they are stored
          whatever it says, and removing one must stay possible. */}
      <div className="flex flex-col gap-4">
        <FieldGroup label={t('settings.resolvers.cookieJars')} hint={t('settings.resolvers.cookieJarsHint')}>
          <div className="glim-well p-0">
            {hosts.length === 0 ? (
              <p className="px-4 py-3 text-sm text-carbon-textMuted">{t('settings.resolvers.cookieEmpty')}</p>
            ) : (
              <ul className="flex flex-col">
                {hosts.map((h, i) => (
                  <li
                    key={h}
                    className={`flex items-center gap-3 px-4 py-2.5 ${
                      i === hosts.length - 1 ? '' : 'border-b border-carbon-border/60'
                    }`}
                  >
                    <span className="text-sm text-carbon-text" dir="ltr">
                      {h}
                    </span>
                    <span className="glim-eyebrow">{t('settings.resolvers.cookieStored')}</span>
                    <IconBadge
                      className="ms-auto"
                      icon={<IconTrash width={16} height={16} />}
                      title={`${t('settings.resolvers.cookieRemove')} · ${h}`}
                      aria-label={`${t('settings.resolvers.cookieRemove')} · ${h}`}
                      disabled={busy}
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
            disabled={busy}
            onClick={() => {
              setError('');
              setAdding(true);
            }}
          >
            {t('cookies.add')}
          </Button>
          {error && <p dir="auto" className="text-xs text-statusWarn">{error}</p>}
        </div>
      </div>

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

import { useEffect, useState } from 'react';
import { type SelfTestRequestView, type SelfTestResult, fetchRequestView } from '../../../lib/api';
import { PROXY_CHECKS, probeWebSocket, proxyVerdicts, wsVerdict } from '../../../lib/selftest';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { Card, SectionTitle } from '../../../components/ui';
import { CheckRow, PROXY_NAMES, adviceKeyFor, useLine } from './rows';

/**
 * ProxyCheckCard compares what the browser sent with what reached this
 * instance, which only the browser can do: a probe from the server would dial
 * its own listener and never touch the proxy. It runs on mount because it only
 * reaches this instance.
 */

export function ProxyCheckCard({ hue }: { hue: number }) {
  const { t } = useT();
  const line = useLine();
  const [view, setView] = useState<SelfTestRequestView | null>(null);
  const [failed, setFailed] = useState(false);
  const [ws, setWs] = useState<boolean | null>(null);

  useEffect(() => {
    let alive = true;
    fetchRequestView().then(
      (v) => {
        if (!alive) return;
        setView(v);
        // The desktop build serves its interface through Wails and has no
        // listener to probe.
        if (v.deployment === 'desktop') return;
        void probeWebSocket().then((ok) => alive && setWs(ok));
      },
      () => alive && setFailed(true),
    );
    return () => {
      alive = false;
    };
  }, []);

  // A failure here is transient while the rest of the page loaded.
  if (failed) return null;

  const desktop = view?.deployment === 'desktop';
  const rows: SelfTestResult[] = view && !desktop ? proxyVerdicts(view, window.location) : [];
  if (view && !desktop && ws !== null) rows.push(wsVerdict(ws));

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.selftest.proxy.hint')}>{t('settings.selftest.proxy.title')}</SectionTitle>

      {desktop ? (
        // The card stays so the cards below keep their hues on every deployment.
        <span className="text-sm text-carbon-textMuted">{t('settings.selftest.proxy.desktopSkip')}</span>
      ) : (
        <div className="flex flex-col">
          {PROXY_CHECKS.map((id) => {
            const res = rows.find((r) => r.id === id);
            const name = PROXY_NAMES[id] ? t(PROXY_NAMES[id] as TranslationKey) : id;
            if (!res) {
              return <CheckRow key={id} name={name} status="pending" sentence={t('settings.selftest.pending')} />;
            }
            const advice = adviceKeyFor(res);
            return (
              <CheckRow
                key={id}
                name={name}
                status={res.status}
                sentence={line(res.code, res.params)}
                advice={advice ? t(advice) : undefined}
              />
            );
          })}
        </div>
      )}
    </Card>
  );
}

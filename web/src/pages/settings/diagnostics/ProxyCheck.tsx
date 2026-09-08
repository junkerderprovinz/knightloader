import { useEffect, useState } from 'react';
import { type SelfTestRequestView, type SelfTestResult, fetchRequestView } from '../../../lib/api';
import { PROXY_CHECKS, probeWebSocket, proxyVerdicts, wsVerdict } from '../../../lib/selftest';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { Card, SectionTitle } from '../../../components/ui';
import { CheckRow, PROXY_NAMES, adviceKeyFor, useLine } from './rows';

/**
 * What the browser and this instance each say about the very same request.
 *
 * THESE FOUR CANNOT BE CHECKED ON THE SERVER, and anybody who tries will get
 * four rows that are always green. A probe run there dials this process's own
 * listener on loopback, never touches the proxy, and reports "WebSocket fine"
 * on an instance whose users have been looking at a frozen list for an hour.
 * The server only ever sees what the proxy handed it; the whole question is
 * whether that matches what the browser sent, and only the browser holds the
 * other half.
 *
 * WHY IT RUNS ON MOUNT WITH NO BUTTON. It costs one small fetch and one socket
 * that is closed the moment it opens, and an operator who came to this page
 * because something is wrong should not have to press anything to be told that
 * the Upgrade header is missing. The card above owns the button because its
 * sweep logs into debrid providers; this one reaches nothing but this instance.
 *
 * THREE OF THE FOUR FAIL WITH NO ERROR ANYWHERE. A rewritten Host header gives
 * a UI that renders perfectly and then answers 403 to every write. A missing
 * X-Forwarded-Proto gives http:// pairing links on an https install. A dropped
 * WebSocket upgrade gives a list that simply stops moving. Not one of them
 * shows up as a message in this app today, which is what this card is for.
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
        // The probe is started only once the deployment is known, so the
        // desktop build never opens one at all: it serves its own interface out
        // of Wails' asset server and opens no TCP listener, so a failed upgrade
        // there says nothing about anybody's nginx.
        if (v.deployment === 'desktop') return;
        void probeWebSocket().then((ok) => alive && setWs(ok));
      },
      () => alive && setFailed(true),
    );
    return () => {
      alive = false;
    };
  }, []);

  // Nothing at all rather than an error card: this instance has just answered
  // seven other routes on the same page, so a failure here is a transient the
  // reader can do nothing with, and a red box for it would be the loudest
  // thing on a diagnostics page that is otherwise reporting good news.
  if (failed) return null;

  const desktop = view?.deployment === 'desktop';
  const rows: SelfTestResult[] = view && !desktop ? proxyVerdicts(view, window.location) : [];
  if (view && !desktop && ws !== null) rows.push(wsVerdict(ws));

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.selftest.proxy.hint')}>{t('settings.selftest.proxy.title')}</SectionTitle>

      {desktop ? (
        // The card stays, and says why there is nothing in it. Dropping it
        // outright would move every card below this one one step along the
        // palette on one deployment and not the other, and a badge sequence
        // that jumps reads as a bug (see ui.tsx's Card). It also leaves the
        // reader wondering where the section they read about went.
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

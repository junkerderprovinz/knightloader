import { useCallback, useEffect, useState } from 'react';
import {
  type SelfTestResult,
  type SelfTestRun,
  fetchRequestView,
  fetchSelfTest,
  startSelfTest,
} from '../../../lib/api';
import { clockSkewMs, fmtSkew, skewStatus } from '../../../lib/selftest';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { fmtBytes, fmtDate } from '../../../lib/format';
import { happened } from '../../../lib/countdown';
import { Button, Card, SectionTitle } from '../../../components/ui';
import { IconRetry } from '../../../lib/icons';
import { CHECK_NAMES, CheckRow, SubRow, adviceKeyFor, useLine } from './rows';

// The instance's own checks: the JDownloader sidecar, yt-dlp, the target
// folders, the debrid logins, the relay, the clock and the torrent port.
//
// The results are polled rather than pushed, because the WebSocket is one of
// the things under test. The browser finishes the clock row itself, timing the
// request echo so a slow round trip does not read as drift (clockSkewMs).

const POLL_MS = 1000;

// Params the server sends as raw byte counts or RFC3339 stamps, formatted here
// in the reader's locale.
const BYTE_PARAMS = ['free', 'mark'];
const TIME_PARAMS = ['time'];

function shown(res: SelfTestResult): Record<string, string> {
  const out: Record<string, string> = { ...(res.params ?? {}) };
  for (const k of BYTE_PARAMS) {
    if (out[k] !== undefined) out[k] = fmtBytes(Number(out[k]));
  }
  for (const k of TIME_PARAMS) {
    if (out[k] !== undefined) out[k] = fmtDate(out[k]) || out[k];
  }
  return out;
}

/**
 * clockRow folds the browser's half into the server's clock result. It says
 * how far apart the clocks are, not which one is wrong, since neither is
 * authoritative.
 */
function clockRow(res: SelfTestResult, skewMs: number | null): SelfTestResult {
  if (skewMs === null) return res;
  const verdict = skewStatus(skewMs);
  if (verdict === 'pass') return res;
  return {
    ...res,
    // The worse status wins; the sentence reports the drift as the more urgent.
    status: verdict === 'fail' ? 'fail' : res.status === 'fail' ? 'fail' : 'warn',
    code: 'clock.skew',
    params: { ...(res.params ?? {}), skew: fmtSkew(skewMs) },
  };
}

export function SelfTestCard({ hue }: { hue: number }) {
  const { t } = useT();
  const line = useLine();
  const [run, setRun] = useState<SelfTestRun | null>(null);
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState('');
  const [skewMs, setSkewMs] = useState<number | null>(null);

  const refresh = useCallback(() => {
    void fetchSelfTest().then(setRun, () => undefined);
  }, []);

  // An instance that was never swept answers a run with an empty id.
  useEffect(refresh, [refresh]);

  const running = run !== null && run.id !== '' && !happened(run.finishedAt);
  useEffect(() => {
    if (!running) return;
    const id = window.setInterval(refresh, POLL_MS);
    return () => window.clearInterval(id);
  }, [running, refresh]);

  async function start() {
    setError('');
    setStarting(true);
    try {
      setRun(await startSelfTest());
      // Started once the sweep runs, so the two calls do not queue on a
      // single-connection proxy. A failure leaves the server's clock row as is.
      const sentAt = Date.now();
      try {
        const view = await fetchRequestView();
        setSkewMs(clockSkewMs(view, sentAt, Date.now()));
      } catch {
        setSkewMs(null);
      }
    } catch (e) {
      setError(t('settings.selftest.startFailed', { error: String(e).replace(/^Error:\s*/, '') }));
    } finally {
      setStarting(false);
    }
  }

  const results = new Map((run?.results ?? []).map((r) => [r.id, r]));
  const planned = run?.planned ?? [];
  const busy = starting || running;

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle
        hint={t('settings.selftest.hint')}
        right={
          <Button onClick={() => void start()} disabled={busy} icon={<IconRetry width={16} height={16} />}>
            {busy ? t('settings.selftest.running') : t('settings.selftest.run')}
          </Button>
        }
      >
        {t('settings.selftest.title')}
      </SectionTitle>

      {planned.length === 0 ? (
        <span className="text-sm text-carbon-textMuted">{t('settings.selftest.never')}</span>
      ) : (
        <div className="flex flex-col">
          {planned.map((id) => {
            const raw = results.get(id);
            const res = raw && id === 'clock' ? clockRow(raw, skewMs) : raw;
            const name = CHECK_NAMES[id] ? t(CHECK_NAMES[id] as TranslationKey) : id;
            if (!res) {
              return <CheckRow key={id} name={name} status="pending" sentence={t('settings.selftest.pending')} />;
            }
            const advice = adviceKeyFor(res);
            return (
              <CheckRow
                key={id}
                name={name}
                status={res.status}
                sentence={line(res.code, shown(res))}
                advice={advice ? t(advice) : undefined}
                detail={res.detail}
              >
                {(res.rows ?? []).map((sub) => {
                  const subAdvice = adviceKeyFor(sub);
                  return (
                    <SubRow
                      key={sub.id}
                      name={roleName(t, sub.params?.role)}
                      status={sub.status}
                      sentence={line(sub.code, shown(sub))}
                      advice={subAdvice ? t(subAdvice) : undefined}
                      detail={sub.detail}
                    />
                  );
                })}
              </CheckRow>
            );
          })}
        </div>
      )}

      {run !== null && happened(run.finishedAt) && (
        <span className="text-[11px] text-carbon-textMuted">
          {t('settings.selftest.lastRun', { when: fmtDate(run.finishedAt) })}
        </span>
      )}
      {error && <span className="text-sm text-statusFail">{error}</span>}
    </Card>
  );
}

/** roleName names a folder row with the disk card's disk.role.* keys. */
function roleName(t: (k: TranslationKey) => string, role: string | undefined): string | undefined {
  if (role === 'downloads') return t('disk.role.downloads');
  if (role === 'work') return t('disk.role.work');
  return undefined;
}

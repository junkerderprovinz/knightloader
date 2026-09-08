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
import { Button, Card, SectionTitle } from '../../../components/ui';
import { IconRetry } from '../../../lib/icons';
import { CHECK_NAMES, CheckRow, SubRow, adviceKeyFor, useLine } from './rows';

/**
 * The instance's own seven checks: the JDownloader sidecar, yt-dlp and its age,
 * the target folders, the debrid logins, the relay, the clock and the torrent
 * port.
 *
 * WHY IT POLLS AND IS NOT PUSHED. internal/hub would make broadcasting these
 * trivial, and doing so would be exactly wrong: the WebSocket is one of the
 * things being tested on the card below this one. A result delivered over it
 * disappears in precisely the case the operator most needs it. So POST answers
 * 202 with a run id and the list of planned checks, this draws all seven rows
 * as waiting, and then asks again every second until finishedAt appears. The
 * interval exists only while something is running - a page that polls a
 * finished sweep for ever is a laptop that gets warm on a settings tab nobody
 * is looking at.
 *
 * NOTHING HERE IS PART OF THE SETTINGS DRAFT. Like the two cards above it,
 * there is nothing to save: only something to read, and one button that asks.
 *
 * THE CLOCK ROW IS FINISHED IN THE BROWSER. The server can report its own time
 * and its own zone; it cannot know how far the reader's clock is from it, and
 * the naive comparison would read half of a slow round trip as drift. So the
 * request echo is fetched with both local timestamps noted and lib/selftest.ts
 * does the arithmetic - see clockSkewMs.
 */

/** How often a running sweep is asked about. */
const POLL_MS = 1000;

/**
 * The params that are BYTES on the wire and words on the screen.
 *
 * The server sends decimal byte counts and never formatted sizes, deliberately:
 * a server that wrote "4,2 GB" would have decided the reader's language and
 * their decimal separator on their behalf, and it has no idea which of the
 * forty-two is loaded.
 */
const BYTE_PARAMS = ['free', 'mark'];

/** The params that are RFC3339 timestamps on the wire. */
const TIME_PARAMS = ['time'];

/** One result's params, ready to be substituted into its sentence. */
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
 * clockRow folds the browser's own half into the server's clock result.
 *
 * The wording never says "your clock is wrong", and that is deliberate rather
 * than diplomatic: neither clock is authoritative here and this app has no way
 * to tell which of the two has drifted. The row states that the two are N
 * apart; the bubble says which to check first, and why (a container takes its
 * time from its host, so the host is where the answer usually is).
 */
function clockRow(res: SelfTestResult, skewMs: number | null): SelfTestResult {
  if (skewMs === null) return res;
  const verdict = skewStatus(skewMs);
  if (verdict === 'pass') return res;
  return {
    ...res,
    // The worse of the two: a UTC zone with a timetable does not stop being
    // worth mentioning because the clocks also disagree, but the disagreement
    // is the more urgent of the two and is what the sentence now says.
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

  // The last sweep, on mount. An instance that has never been swept answers a
  // run with an empty id, which is what "not run yet" is made of - the route
  // deliberately does not 404 there, so this needs no special case.
  useEffect(refresh, [refresh]);

  const running = run !== null && run.id !== '' && !run.finishedAt;
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
      // Timed, and started only once the sweep is under way so the two calls
      // do not queue behind each other on a single-connection proxy. A failure
      // here leaves the clock row exactly as the server reported it rather
      // than failing the whole sweep: the other six checks are still worth
      // having, and the drift is the one thing the browser adds.
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

      {run?.finishedAt && (
        <span className="text-[11px] text-carbon-textMuted">
          {t('settings.selftest.lastRun', { when: fmtDate(run.finishedAt) })}
        </span>
      )}
      {error && <span className="text-sm text-statusFail">{error}</span>}
    </Card>
  );
}

/**
 * A folder row's own name, from the role the disk readout already labels its
 * volumes with.
 *
 * Reused rather than given keys of its own: disk.role.downloads and
 * disk.role.work are already in all forty-two locales and already name exactly
 * these two folders on the Overview page's disk card. Two new keys saying the
 * same words would be two more things to translate and one more place for the
 * same folder to be called something else.
 */
function roleName(t: (k: TranslationKey) => string, role: string | undefined): string | undefined {
  if (role === 'downloads') return t('disk.role.downloads');
  if (role === 'work') return t('disk.role.work');
  return undefined;
}

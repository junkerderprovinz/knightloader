import { useEffect, useState, type ReactNode } from 'react';
import type { SelfTestResult } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { fmtBytes, fmtDate } from '../../../lib/format';
import { happened } from '../../../lib/countdown';
import type { SelfTest } from '../../../lib/useSelfTest';
import { Button, type ButtonVerdict } from '../../../components/ui';
import { Pulse, type PulseCheck } from '../../../components/pictures/Pulse';
import { IconCheck, IconClose, IconWarning } from '../../../lib/icons';
import { Fold, VERDICT_WORDS, type FoldVerdict } from '../health/Fold';
import { CHECK_NAMES, CheckRow, STATUS_WORDS, SubRow, adviceKeyFor, useLine } from './rows';

// The instance's own checks: the JDownloader sidecar, yt-dlp, the target
// folders, the debrid logins, the relay, the clock and the torrent port.

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
 * SelfTestButton starts a run and wears its answer: the worst row of the run
 * it started, with the clock row as the browser finished it. A run found on
 * arrival leaves the button as it was.
 */
export function SelfTestButton({ test }: { test: SelfTest }) {
  const { t } = useT();
  // The failure counter of the button, so a repeated failure shakes it again.
  const [shake, setShake] = useState(0);
  const { run, busy, error, mine, statuses } = test;

  const outcome: ButtonVerdict | null =
    !mine || busy || run === null || !happened(run.finishedAt)
      ? null
      : statuses.includes('fail')
        ? 'fail'
        : statuses.includes('warn')
          ? 'warn'
          : 'ok';
  useEffect(() => {
    if (outcome === 'fail') setShake((n) => n + 1);
  }, [outcome]);
  const face: { label: string; icon: ReactNode; verdict?: ButtonVerdict } = busy
    ? { label: t('settings.selftest.running'), icon: <IconCheck /> }
    : error
      ? { label: t('settings.info.checkFailed'), icon: <IconClose />, verdict: 'fail' }
      : outcome === 'ok'
        ? { label: t('settings.diagnostics.verdict.ok'), icon: <IconCheck />, verdict: 'ok' }
        : outcome === 'warn'
          ? { label: t('settings.diagnostics.verdict.warn'), icon: <IconWarning />, verdict: 'warn' }
          : outcome === 'fail'
            ? { label: t('settings.diagnostics.verdict.fail'), icon: <IconClose />, verdict: 'fail' }
            : { label: t('settings.selftest.run'), icon: <IconCheck /> };

  return (
    <>
      <Button
        kind="secondary"
        icon={face.icon}
        verdict={face.verdict}
        shake={shake}
        disabled={busy}
        onClick={() => void test.start().then((started) => started || setShake((n) => n + 1))}
      >
        {face.label}
      </Button>
      <span role="status" className="sr-only">
        {face.verdict ? face.label : ''}
      </span>
    </>
  );
}

/**
 * SelfTestFold is the self-test on the Health card: the pulse and every
 * check's sentence with its advice. It stands open while a run is going, after
 * one that found something, and until there has been one.
 */
export function SelfTestFold({ test }: { test: SelfTest }) {
  const { t } = useT();
  const line = useLine();
  const { run, error } = test;
  const planned = run?.planned ?? [];
  const finished = run !== null && happened(run.finishedAt);
  const verdict: FoldVerdict | undefined =
    !finished || planned.length === 0
      ? undefined
      : test.statuses.includes('fail')
        ? 'fail'
        : test.statuses.includes('warn')
          ? 'warn'
          : 'ok';

  const beads: PulseCheck[] = planned.map((id) => {
    const res = test.resultOf(id);
    return {
      id,
      name: CHECK_NAMES[id] ? t(CHECK_NAMES[id]) : id,
      status: res?.status ?? 'pending',
      word: res ? t(STATUS_WORDS[res.status]) : t('settings.selftest.pending'),
    };
  });

  return (
    <Fold
      label={t('settings.selftest.title')}
      hint={t('settings.selftest.hint')}
      verdict={verdict}
      verdictWord={verdict && t(VERDICT_WORDS[verdict])}
      open={test.running || planned.length === 0 || verdict === 'fail' || verdict === 'warn'}
    >
      {planned.length === 0 ? (
        <span className="text-sm text-carbon-textMuted">{t('settings.selftest.never')}</span>
      ) : (
        <>
          <Pulse checks={beads} running={test.running} label={t('settings.selftest.title')} />
          <div className="flex flex-col">
            {planned.map((id) => {
              const res = test.resultOf(id);
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
        </>
      )}

      {finished && run && (
        <span className="text-meta text-carbon-textMuted">
          {t('settings.selftest.lastRun', { when: fmtDate(run.finishedAt) })}
        </span>
      )}
      {error && <span className="text-sm text-carbon-textSub">{t('settings.selftest.startFailed', { error })}</span>}
    </Fold>
  );
}

/** roleName names a folder row with the disk card's disk.role.* keys. */
function roleName(t: (k: TranslationKey) => string, role: string | undefined): string | undefined {
  if (role === 'downloads') return t('disk.role.downloads');
  if (role === 'work') return t('disk.role.work');
  return undefined;
}

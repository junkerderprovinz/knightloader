import { useState, type ComponentType, type SVGProps } from 'react';
import { type StartupCheck, type StartupReport, runStartupCheck } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { fmtDate } from '../../../lib/format';
import { adviceFor } from '../../../lib/startupAdvice';
import { Card, InfoBubble, SectionTitle, type ButtonVerdict } from '../../../components/ui';
import { TestButton } from '../../../components/TestButton';
import { IconCheck, IconClose, IconWarning } from '../../../lib/icons';

// The start report shows what this instance checked once after it came up. It
// takes the report from the diagnostics bundle the page already loaded. A fresh
// check is held in the component only; the server keeps the boot reading for
// bug reports (App.RunStartupCheckNow). LabelBadge has two tones and this needs
// four, so the tones are local like StatusPill's.

type Tone = 'ok' | 'warn' | 'fail' | 'neutral';

const toneText: Record<Tone, string> = {
  ok: 'text-statusOk',
  warn: 'text-statusWarn',
  fail: 'text-statusFail',
  neutral: 'text-statusNeutral',
};

/** A short bar for a skipped row, which none of the shared glyphs fits. */
const GlyphSkipped = (p: SVGProps<SVGSVGElement>) => (
  <svg width={22} height={22} viewBox="0 0 20 20" fill="currentColor" className="shrink-0" aria-hidden {...p}>
    <rect x="4" y="9" width="12" height="2" rx="1" />
  </svg>
);

/** The startupcheck.Verdict ids; an unknown one draws as a neutral row. */
const verdicts: Record<string, { tone: Tone; glyph: ComponentType<SVGProps<SVGSVGElement>>; key: TranslationKey }> = {
  ok: { tone: 'ok', glyph: IconCheck, key: 'settings.diagnostics.verdict.ok' },
  warn: { tone: 'warn', glyph: IconWarning, key: 'settings.diagnostics.verdict.warn' },
  fail: { tone: 'fail', glyph: IconClose, key: 'settings.diagnostics.verdict.fail' },
  skipped: { tone: 'neutral', glyph: GlyphSkipped, key: 'settings.diagnostics.verdict.skipped' },
};

/** Row names by check id; folder rows are named by role in rowName. */
const checkNames: Record<string, TranslationKey> = {
  data: 'settings.diagnostics.check.data',
  java: 'settings.diagnostics.check.java',
  ytdlp: 'settings.diagnostics.check.ytdlp',
  ffmpeg: 'settings.diagnostics.check.ffmpeg',
  ffprobe: 'settings.diagnostics.check.ffprobe',
  clock: 'settings.diagnostics.check.clock',
  settings: 'settings.diagnostics.check.settings',
};

const roleNames: Record<string, TranslationKey> = {
  downloads: 'settings.diagnostics.role.downloads',
  work: 'settings.diagnostics.role.work',
  category: 'settings.diagnostics.role.category',
  extract: 'settings.diagnostics.role.extract',
  extractMove: 'settings.diagnostics.role.extractMove',
  watch: 'settings.diagnostics.role.watch',
};

export function StartupReportCard({ hue, report }: { hue: number; report: StartupReport | null }) {
  const { t } = useT();
  // Null while the card shows the boot reading.
  const [fresh, setFresh] = useState<StartupReport | null>(null);
  const [error, setError] = useState('');

  const shown = fresh ?? report;

  // The worst row decides what the button shows; the rows say which and why.
  async function onRecheck(): Promise<ButtonVerdict> {
    setError('');
    try {
      const next = await runStartupCheck();
      setFresh(next);
      if (next.checks.some((c) => c.verdict === 'fail')) return 'fail';
      return next.checks.some((c) => c.verdict === 'warn') ? 'warn' : 'ok';
    } catch (e) {
      setError(t('settings.diagnostics.startupRecheckFailed', { error: String(e).replace(/^Error:\s*/, '') }));
      return 'fail';
    }
  }

  const nothingWrong =
    shown !== null &&
    shown.state === 'done' &&
    shown.checks.length > 0 &&
    shown.checks.every((c) => c.verdict === 'ok' || c.verdict === 'skipped');

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.diagnostics.startupHint')}>{t('settings.diagnostics.startupTitle')}</SectionTitle>

      {/* Each state gets its own sentence, so "nothing was checked" never
          looks like "nothing was wrong". */}
      {shown === null && <Line>{t('settings.diagnostics.startupNotRun')}</Line>}
      {shown?.state === 'off' && <Line>{t('settings.diagnostics.startupOff')}</Line>}
      {shown?.state === 'running' && <Line>{t('settings.diagnostics.startupRunning')}</Line>}

      {shown !== null && shown.state !== 'off' && (
        <Line>
          {fresh
            ? t('settings.diagnostics.startupJustChecked')
            : t('settings.diagnostics.startupTakenAt', { when: fmtDate(shown.startedAt) })}
        </Line>
      )}

      {/* Without a probe, the folder rows below rest on a stat alone. */}
      {shown !== null && shown.state === 'done' && !shown.probed && (
        <Line>{t('settings.diagnostics.startupStatOnly')}</Line>
      )}

      {shown !== null && shown.checks.length > 0 && (
        <ul className="flex flex-col gap-3">
          {shown.checks.map((c, i) => (
            <Row key={`${c.id}-${c.role ?? ''}-${i}`} check={c} />
          ))}
        </ul>
      )}

      {nothingWrong && <span className="text-sm text-statusOk">{t('settings.diagnostics.startupNothingWrong')}</span>}

      {/* Why the check could not run, above the button until the next one. */}
      {error && <span className="text-sm text-carbon-textSub">{error}</span>}

      <div className="flex flex-wrap items-center gap-1.5">
        <TestButton
          label={t('settings.diagnostics.startupRecheck')}
          busyLabel={t('settings.diagnostics.startupRechecking')}
          hint={t('settings.diagnostics.startupRecheckHint')}
          words={{
            ok: t('settings.diagnostics.verdict.ok'),
            warn: t('settings.diagnostics.verdict.warn'),
            fail: error ? t('settings.info.checkFailed') : t('settings.diagnostics.verdict.fail'),
          }}
          run={onRecheck}
        />
      </div>
    </Card>
  );
}

function Line({ children }: { children: string }) {
  return <span className="text-sm text-carbon-textSub">{children}</span>;
}

/** Row shows one check; the remedy sits behind an (i) so it does not bury the rows that pass. */
function Row({ check }: { check: StartupCheck }) {
  const { t } = useT();
  const v = verdicts[check.verdict] ?? { tone: 'neutral' as Tone, glyph: GlyphSkipped, key: undefined };
  const Glyph = v.glyph;
  const advice = adviceFor(check.id, check.code);
  const name = rowName(t, check);

  return (
    <li className="flex flex-col gap-0.5">
      <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <span className={`flex items-center gap-1.5 ${toneText[v.tone]}`}>
          <Glyph width={16} height={16} />
          <span className="text-sm">{name}</span>
        </span>
        <span className={`text-meta ${toneText[v.tone]}`}>{v.key ? t(v.key) : check.verdict}</span>
        {advice && <InfoBubble tip={t(advice, adviceVars(check))} />}
      </span>

      {check.subject && (
        <span className="glim-num break-all text-meta text-carbon-textMuted" dir="ltr">
          {check.subject}
        </span>
      )}
      {/* A missing folder was measured somewhere else, often another disk. */}
      {check.measured && check.measured !== check.subject && (
        <span className="glim-num break-all text-meta text-carbon-textMuted" dir="ltr">
          {t('settings.diagnostics.startupNearest', { measured: check.measured })}
        </span>
      )}
      {check.detail && (
        <span className="glim-num break-all text-meta text-carbon-textSub" dir="ltr">
          {check.detail}
        </span>
      )}
      {/* Raw, so the error stays searchable. */}
      {check.err && (
        <span className="break-all font-mono text-meta text-carbon-textMuted" dir="ltr">
          {check.err}
        </span>
      )}
    </li>
  );
}

/**
 * rowName names a row, a folder row by its role. An unknown id or role falls
 * back to the raw string.
 */
function rowName(t: (key: TranslationKey, vars?: Record<string, string | number>) => string, check: StartupCheck): string {
  if (check.id === 'folder') {
    const key = check.role ? roleNames[check.role] : undefined;
    return key ? t(key) : (check.role ?? check.id);
  }
  const key = checkNames[check.id];
  return key ? t(key) : check.id;
}

/** adviceVars fills a remedy sentence; missing values become empty strings. */
function adviceVars(check: StartupCheck): Record<string, string> {
  return {
    measured: check.measured ?? '',
    subject: check.subject ?? '',
    error: check.err ?? '',
  };
}

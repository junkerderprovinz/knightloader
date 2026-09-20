import { useState, type ComponentType, type SVGProps } from 'react';
import { type StartupCheck, type StartupReport, runStartupCheck } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { fmtDate } from '../../../lib/format';
import { adviceFor } from '../../../lib/startupAdvice';
import { Button, Card, InfoBubble, SectionTitle } from '../../../components/ui';
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
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const shown = fresh ?? report;

  async function onRecheck() {
    setError('');
    setBusy(true);
    try {
      setFresh(await runStartupCheck());
    } catch (e) {
      setError(t('settings.diagnostics.startupRecheckFailed', { error: String(e).replace(/^Error:\s*/, '') }));
    } finally {
      setBusy(false);
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

      <div className="flex flex-wrap items-center gap-1.5">
        <Button kind="secondary" hue={hue} disabled={busy} onClick={() => void onRecheck()}>
          {busy ? t('settings.diagnostics.startupRechecking') : t('settings.diagnostics.startupRecheck')}
        </Button>
        {/* A sibling rather than a title, which the keyboard cannot open. */}
        <InfoBubble tip={t('settings.diagnostics.startupRecheckHint')} />
      </div>

      {error && <span className="text-sm text-statusFail">{error}</span>}
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
        <span className={`text-[11px] ${toneText[v.tone]}`}>{v.key ? t(v.key) : check.verdict}</span>
        {advice && <InfoBubble tip={t(advice, adviceVars(check))} />}
      </span>

      {check.subject && (
        <span className="glim-num break-all text-[11px] text-carbon-textMuted" dir="ltr">
          {check.subject}
        </span>
      )}
      {/* A missing folder was measured somewhere else, often another disk. */}
      {check.measured && check.measured !== check.subject && (
        <span className="glim-num break-all text-[11px] text-carbon-textMuted" dir="ltr">
          {t('settings.diagnostics.startupNearest', { measured: check.measured })}
        </span>
      )}
      {check.detail && (
        <span className="glim-num break-all text-[11px] text-carbon-textSub" dir="ltr">
          {check.detail}
        </span>
      )}
      {/* Raw, so the error stays searchable. */}
      {check.err && (
        <span className="break-all font-mono text-[11px] text-carbon-textMuted" dir="ltr">
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

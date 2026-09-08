import { useState, type ComponentType, type SVGProps } from 'react';
import { type StartupCheck, type StartupReport, runStartupCheck } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { fmtDate } from '../../../lib/format';
import { adviceFor } from '../../../lib/startupAdvice';
import { Button, Card, InfoBubble, SectionTitle } from '../../../components/ui';
import { IconCheck, IconClose, IconWarning } from '../../../lib/icons';

/**
 * The start report: what this instance checked once, just after it came up.
 *
 * WHY IT IS ON THIS PAGE. Every row here is already in the diagnostics bundle
 * the card above builds, so the reading and the file that carries it belong
 * together; splitting them would put the finding on one page and the evidence on
 * another.
 *
 * NOTHING IS FETCHED HERE. The report arrives as a prop from the bundle the page
 * has already loaded. A second GET would be a second answer to the same
 * question, and two ways of reading one document are two things that can
 * disagree - the exact argument buildDiagnostics makes for taking the speed
 * counts and the database sizes from the calls that already own them.
 *
 * PRESSING "CHECK AGAIN" DOES NOT REPLACE THE BUNDLE'S READING, and the card
 * says so out loud. The server keeps the boot reading on purpose (see
 * App.RunStartupCheckNow): what was true at start is what a bug report needs,
 * and pressing this button is exactly when somebody would destroy it. So the
 * fresh answer is held here, in this component, and the downloaded file goes on
 * carrying the one taken at start.
 *
 * THE TONES ARE LOCAL. LabelBadge's `tone` is 'ok' | 'fail' and this needs four,
 * and widening that union in ui.tsx would be a merge conflict in a file half the
 * interface is editing. StatusPill.tsx already keeps its own map of exactly this
 * shape for exactly this reason.
 */

type Tone = 'ok' | 'warn' | 'fail' | 'neutral';

const toneText: Record<Tone, string> = {
  ok: 'text-statusOk',
  warn: 'text-statusWarn',
  fail: 'text-statusFail',
  neutral: 'text-statusNeutral',
};

/**
 * A short horizontal bar for "not needed here", drawn here rather than added to
 * lib/icons.tsx.
 *
 * None of the shared glyphs is honest for a skipped row: a tick claims it
 * passed, a cross claims it failed, and a pause claims something is waiting.
 * Same viewBox, same fill and the same aria-hidden as every icon in that file,
 * so it lines up with its neighbours in a row.
 */
const GlyphSkipped = (p: SVGProps<SVGSVGElement>) => (
  <svg width={22} height={22} viewBox="0 0 20 20" fill="currentColor" className="shrink-0" aria-hidden {...p}>
    <rect x="4" y="9" width="12" height="2" rx="1" />
  </svg>
);

/** The verdict ids the server sends (startupcheck.Verdict), each with its tone,
 *  its glyph and the word for it. An id this build has never heard of falls
 *  through to a neutral row that still draws, rather than to nothing. */
const verdicts: Record<string, { tone: Tone; glyph: ComponentType<SVGProps<SVGSVGElement>>; key: TranslationKey }> = {
  ok: { tone: 'ok', glyph: IconCheck, key: 'settings.diagnostics.verdict.ok' },
  warn: { tone: 'warn', glyph: IconWarning, key: 'settings.diagnostics.verdict.warn' },
  fail: { tone: 'fail', glyph: IconClose, key: 'settings.diagnostics.verdict.fail' },
  skipped: { tone: 'neutral', glyph: GlyphSkipped, key: 'settings.diagnostics.verdict.skipped' },
};

/** The row names, by what the server said the row was about. Folder rows are
 *  named by their ROLE instead - see rowName. */
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
  // The answer to a press, held here and nowhere else. Null means the card is
  // still showing the boot reading, which is the state it opens in.
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

      {/* Four states, and each one gets a sentence of its own. An empty check
          list drawn as a clean bill of health is the worst thing this card
          could do: "nothing was looked at" and "nothing was wrong" would then
          be the same picture. */}
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

      {/* A fact about what the pass did, so it is a line and not a bubble - the
          same call MaintenanceCard's "next run" line makes. It matters because
          every folder row below says "fine" on the strength of a stat alone
          when this is showing. */}
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
        {/* The (i) is a sibling of the button and not a title on it: a native
            tooltip does not open on focus and cannot be read by anybody using
            the keyboard alone. Same arrangement as the maintenance actions. */}
        <InfoBubble tip={t('settings.diagnostics.startupRecheckHint')} />
      </div>

      {error && <span className="text-sm text-statusFail">{error}</span>}
    </Card>
  );
}

/** One plain sentence about the pass as a whole. */
function Line({ children }: { children: string }) {
  return <span className="text-sm text-carbon-textSub">{children}</span>;
}

/**
 * One thing the pass looked at.
 *
 * The name, the verdict word and the glyph are the part read at a glance; the
 * path or the version line is the part read second; the remedy is behind an (i),
 * because it is a paragraph and a paragraph per row would bury the eight rows
 * that are fine.
 */
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

      {/* ltr regardless of interface direction, the convention every path, URL
          and code cell in settings/ already follows: these are paths, binary
          names and version strings, none of which reads correctly mirrored. */}
      {check.subject && (
        <span className="glim-num break-all text-[11px] text-carbon-textMuted" dir="ltr">
          {check.subject}
        </span>
      )}
      {/* The substitution has to be SEEN rather than trusted. A folder that is
          not there was measured somewhere else, and on a box that mounts a
          share that somewhere else is usually the wrong disk entirely - the
          same distinction DiskReport.Measured draws. */}
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
      {/* Shown raw and never translated: it is the system's own words, and a
          translated errno is neither searchable nor quotable in a bug report. */}
      {check.err && (
        <span className="break-all font-mono text-[11px] text-carbon-textMuted" dir="ltr">
          {check.err}
        </span>
      )}
    </li>
  );
}

/**
 * What to call a row.
 *
 * A folder row is named by its ROLE and not by "folder", because there are as
 * many of those as somebody configured and "Folder, Folder, Folder" names none
 * of them. An id or a role this build has never heard of falls back to the raw
 * string the server sent: a row nobody can name is still a row worth drawing,
 * and it is a good deal better than a blank cell.
 */
function rowName(t: (key: TranslationKey, vars?: Record<string, string | number>) => string, check: StartupCheck): string {
  if (check.id === 'folder') {
    const key = check.role ? roleNames[check.role] : undefined;
    return key ? t(key) : (check.role ?? check.id);
  }
  const key = checkNames[check.id];
  return key ? t(key) : check.id;
}

/** The values a remedy sentence can name. Every one of them is optional on the
 *  wire, so each falls back to an empty string rather than the word "undefined"
 *  appearing in the middle of a sentence. */
function adviceVars(check: StartupCheck): Record<string, string> {
  return {
    measured: check.measured ?? '',
    subject: check.subject ?? '',
    error: check.err ?? '',
  };
}

import { useCallback, type ReactNode } from 'react';
import type { SelfTestResult, SelfTestStatus } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { InfoBubble } from '../../../components/ui';
import { IconCheck, IconClock, IconClose, IconHelp, IconWarning } from '../../../lib/icons';

/**
 * One self-test row, drawn the same way whether the server answered it or the
 * browser did.
 *
 * WHY ONE COMPONENT FOR BOTH CARDS. The seven instance checks come back from
 * GET /api/selftest and the four proxy checks are worked out in the browser
 * (lib/selftest.ts), but they arrive in the same shape - a status and a stable
 * code - and they are the same thing to a reader: a name, a verdict, and an (i)
 * with what to do about it. Two components would be two treatments of one idea,
 * and the second one would drift.
 *
 * THE ADVICE IS IN THE BUBBLE AND NOWHERE ELSE. That is the house rule, and it
 * is also simply right here: the advice is three sentences about nginx
 * directives or container environment variables, and nobody wants them on
 * screen when the row says "Fine". The verdict itself stays one line.
 *
 * NO KEY IN THIS FILE IS A `label` OR A `hint` PROP, deliberately. Those two
 * attribute names are what check-settings-search.mjs scans for, and a row here
 * is a readout rather than a control - it has no caption in the DOM for the
 * settings search to jump to. The searchable handles for these two cards are
 * their titles and the check NAMES, which searchIndex.ts carries under `also`.
 */

/** The seven instance checks, by the id the server sends. */
export const CHECK_NAMES: Record<string, TranslationKey> = {
  jd: 'settings.selftest.jd',
  ytdlp: 'settings.selftest.ytdlp',
  folders: 'settings.selftest.folders',
  accounts: 'settings.selftest.accounts',
  relay: 'settings.selftest.relay',
  clock: 'settings.selftest.clock',
  torrentPort: 'settings.selftest.torrentPort',
};

/** The four proxy checks, by the id lib/selftest.ts gives them. */
export const PROXY_NAMES: Record<string, TranslationKey> = {
  host: 'settings.selftest.proxy.host',
  proto: 'settings.selftest.proxy.proto',
  prefix: 'settings.selftest.proxy.prefix',
  ws: 'settings.selftest.proxy.ws',
};

/** The status word beside a row's name. */
const STATUS_WORDS: Record<SelfTestStatus, TranslationKey> = {
  pass: 'settings.selftest.status.pass',
  warn: 'settings.selftest.status.warn',
  fail: 'settings.selftest.status.fail',
  skipped: 'settings.selftest.status.skipped',
  unknown: 'settings.selftest.status.unknown',
};

/**
 * Which verdicts carry advice, and which advice.
 *
 * Written out rather than derived by sticking "Advice" on the end of the code,
 * for two reasons that both bite. A key built by concatenation cannot be
 * checked by anything - a typo resolves to undefined and the bubble renders the
 * word "undefined" - and two different verdicts legitimately share one piece of
 * advice: an ageing yt-dlp and an old one need exactly the same thing done
 * about them, and writing that sentence twice is two sentences to keep in step.
 */
const ADVICE: Record<string, TranslationKey> = {
  'jd.missing': 'settings.selftest.jd.missingAdvice',
  'jd.unreachable': 'settings.selftest.jd.unreachableAdvice',
  'ytdlp.missing': 'settings.selftest.ytdlp.missingAdvice',
  'ytdlp.aging': 'settings.selftest.ytdlp.oldAdvice',
  'ytdlp.old': 'settings.selftest.ytdlp.oldAdvice',
  'folders.notWritable': 'settings.selftest.folders.notWritableAdvice',
  'folders.missing': 'settings.selftest.folders.missingAdvice',
  'accounts.allOk': 'settings.selftest.accounts.readOnlyHint',
  'accounts.someFailed': 'settings.selftest.accounts.readOnlyHint',
  'relay.notConnected': 'settings.selftest.relay.notConnectedAdvice',
  'clock.skew': 'settings.selftest.clock.skewAdvice',
  'clock.utc': 'settings.selftest.clock.utcAdvice',
  'torrentPort.noPort': 'settings.selftest.torrentPort.noPortAdvice',
  'torrentPort.notChecked': 'settings.selftest.torrentPort.notCheckedAdvice',
  'proxy.host.rewritten': 'settings.selftest.proxy.host.rewrittenAdvice',
  'proxy.proto.missing': 'settings.selftest.proxy.proto.missingAdvice',
  'proxy.prefix.underPath': 'settings.selftest.proxy.prefix.underPathAdvice',
  'proxy.ws.failed': 'settings.selftest.proxy.ws.failedAdvice',
};

/**
 * A refused login's advice, chosen by the CACHED health state the server sent
 * along with the row.
 *
 * The four next steps are genuinely different and only one of them is "get a
 * new key", so a single sentence for all of them would be wrong three times out
 * of four. The state is a pure cache read on the server - see
 * selfTestAccountsRO - so this costs nothing and guesses nothing.
 */
const ACCOUNT_ADVICE: Record<string, TranslationKey> = {
  invalid: 'settings.selftest.accounts.advice.invalid',
  expired: 'settings.selftest.accounts.advice.expired',
  temp_disabled: 'settings.selftest.accounts.advice.temp',
  error: 'settings.selftest.accounts.advice.error',
};

/**
 * useLine resolves a code into the sentence for it, with the substitutions the
 * server sent.
 *
 * IT SUBSTITUTES ITSELF RATHER THAN HANDING THE VARS TO `t`, and that is not a
 * preference. i18n.tsx's `t` does `dict[key] ?? en[key]` with no final fallback
 * and then calls `.replaceAll` on the result, so a key the catalogue does not
 * have throws a TypeError and blanks the whole page. Codes cross the wire from
 * a server that may be a version ahead of this bundle, so an unknown one is a
 * thing that can actually happen - and when it does, showing the raw code is a
 * bad row, while blanking the settings page is a broken app.
 */
export function useLine() {
  const { t } = useT();
  return useCallback(
    (code: string, params?: Record<string, string>): string => {
      const raw = t(`settings.selftest.${code}` as TranslationKey) as string | undefined;
      if (raw === undefined) return code;
      let s = raw;
      if (params) for (const [k, v] of Object.entries(params)) s = s.replaceAll(`{${k}}`, v);
      return s;
    },
    [t],
  );
}

/** The advice for one result, or nothing when the verdict needs none. */
export function adviceKeyFor(res: SelfTestResult): TranslationKey | undefined {
  if (res.code === 'accounts.rowFailed') {
    return ACCOUNT_ADVICE[res.params?.health ?? ''];
  }
  return ADVICE[res.code];
}

/**
 * The glyph. Five states and five treatments, because five is what the
 * vocabulary has: "nothing is configured here" and "it is configured and this
 * build cannot find out" are different answers and must not look the same.
 *
 * Every glyph is one already in lib/icons.tsx except the skipped one, which is
 * a ring drawn in CSS - an empty circle is what "there was nothing to check"
 * looks like, and none of the existing icons says that without also saying
 * something else.
 */
function StatusGlyph({ status }: { status: SelfTestStatus | 'pending' }) {
  const size = { width: 16, height: 16 };
  switch (status) {
    case 'pass':
      return <IconCheck {...size} className="shrink-0 text-statusOk" />;
    case 'warn':
      return <IconWarning {...size} className="shrink-0 text-statusWarn" />;
    case 'fail':
      return <IconClose {...size} className="shrink-0 text-statusFail" />;
    case 'unknown':
      return <IconHelp {...size} className="shrink-0 text-statusInfo" />;
    case 'pending':
      return <IconClock {...size} className="shrink-0 text-carbon-textMuted" />;
    default:
      return <span className="mt-0.5 size-3 shrink-0 rounded-full border border-carbon-border" />;
  }
}

/**
 * CheckRow is one line of the report.
 *
 * `detail` is the OTHER side's own words - a Go error, a provider's refusal, a
 * router's fault string - and gets the same monospace, always-ltr treatment
 * every path, URL and log line in settings/ already gets: those strings mix
 * hostnames, paths and stack traces, none of which read correctly mirrored in
 * an interface running right to left.
 */
export function CheckRow({
  name,
  status,
  sentence,
  advice,
  detail,
  children,
}: {
  name: string;
  status: SelfTestStatus | 'pending';
  sentence: string;
  advice?: string;
  detail?: string;
  children?: ReactNode;
}) {
  const { t } = useT();
  return (
    <div className="flex items-start gap-3 border-t border-carbon-border py-3 first:border-t-0 first:pt-0">
      <span className="mt-0.5 flex">
        <StatusGlyph status={status} />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
          <span className="text-sm font-medium text-carbon-text">{name}</span>
          <span className="glim-eyebrow text-[11px] text-carbon-textMuted">
            {status === 'pending' ? t('settings.selftest.pending') : t(STATUS_WORDS[status])}
          </span>
        </div>
        <span className="flex items-start gap-1 text-sm text-carbon-textSub">
          <span className="min-w-0">{sentence}</span>
          {advice && <InfoBubble tip={advice} />}
        </span>
        {detail && (
          <span
            dir="ltr"
            className="max-w-full overflow-x-auto whitespace-pre-wrap break-all font-mono text-[11px] leading-relaxed text-carbon-textMuted"
          >
            {detail}
          </span>
        )}
        {children && <div className="mt-1 flex flex-col gap-2 border-l border-carbon-border pl-3">{children}</div>}
      </div>
    </div>
  );
}

/**
 * SubRow is one of a check's own rows: one debrid account, one folder. One
 * level of nesting and no more - a tree would need a tree renderer, and the
 * parent already carries the summary.
 */
export function SubRow({
  name,
  status,
  sentence,
  advice,
  detail,
}: {
  /** Optional: a folder row is named ("Working folder"), an account row is not
   *  - its own sentence starts with the account's label, and a second name
   *  above it would print the same word twice. */
  name?: string;
  status: SelfTestStatus;
  sentence: string;
  advice?: string;
  detail?: string;
}) {
  return (
    <div className="flex items-start gap-2">
      <span className="mt-0.5 flex">
        <StatusGlyph status={status} />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        {name && <span className="glim-eyebrow text-[11px] text-carbon-textMuted">{name}</span>}
        <span className="flex items-start gap-1 text-sm text-carbon-textSub">
          <span className="min-w-0">{sentence}</span>
          {advice && <InfoBubble tip={advice} />}
        </span>
        {detail && (
          <span
            dir="ltr"
            className="max-w-full overflow-x-auto whitespace-pre-wrap break-all font-mono text-[11px] leading-relaxed text-carbon-textMuted"
          >
            {detail}
          </span>
        )}
      </div>
    </div>
  );
}

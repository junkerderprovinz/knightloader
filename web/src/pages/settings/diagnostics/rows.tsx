import { useCallback, type ReactNode } from 'react';
import type { SelfTestResult, SelfTestStatus } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { interpolate } from '../../../lib/interpolate';
import { InfoBubble } from '../../../components/ui';
import { IconCheck, IconClock, IconClose, IconHelp, IconWarning } from '../../../lib/icons';

// Self-test rows, drawn the same way whether the server or the browser
// answered them, with the advice behind an (i). No prop here is called `label`
// or `hint`, because check-settings-search.mjs treats those as searchable
// captions; the check names are in searchIndex.ts under `also`.

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
 * The advice per verdict, written out so the keys are type-checked and two
 * verdicts can share one sentence.
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

/** A refused login's advice, by the cached health state sent with the row. */
const ACCOUNT_ADVICE: Record<string, TranslationKey> = {
  invalid: 'settings.selftest.accounts.advice.invalid',
  expired: 'settings.selftest.accounts.advice.expired',
  temp_disabled: 'settings.selftest.accounts.advice.temp',
  error: 'settings.selftest.accounts.advice.error',
};

/**
 * useLine resolves a code into its sentence with the server's substitutions.
 * It substitutes itself because `t` throws on a key the catalogue lacks, and a
 * newer server can send one.
 */
export function useLine() {
  const { t } = useT();
  return useCallback(
    (code: string, params?: Record<string, string>): string => {
      const raw = t(`settings.selftest.${code}` as TranslationKey) as string | undefined;
      return raw === undefined ? code : interpolate(raw, params);
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
 * StatusGlyph draws one glyph per state. Skipped is a CSS ring, since no shared
 * icon says "nothing to check".
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
 * CheckRow is one line of the report. `detail` is the other side's own message
 * and is shown in monospace, left to right.
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
        {children && <div className="mt-1 flex flex-col gap-2 border-s border-carbon-border ps-3">{children}</div>}
      </div>
    </div>
  );
}

/** SubRow is one of a check's own rows, such as one account or one folder. */
export function SubRow({
  name,
  status,
  sentence,
  advice,
  detail,
}: {
  /** Set for folder rows; an account row's sentence already starts with its label. */
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

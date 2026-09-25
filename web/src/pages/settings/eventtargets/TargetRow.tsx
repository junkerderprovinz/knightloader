import { useEffect, useState } from 'react';
import { Field, FieldGroup, IconBadge, NumberInput, TextArea, TextInput, ToggleRow, useTooltip } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { IconTrash } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import {
  DEFAULT_TIMEOUT_SECONDS,
  MAX_ATTEMPTS,
  MAX_TIMEOUT_SECONDS,
  balancedTemplate,
  formatHeaders,
  hostOf,
  parseHeaders,
  usableAddress,
  type EventTargetRow,
  type EventTargetStatus,
  type Placeholder,
  type TargetMethod,
} from '../../../lib/eventtargets';
import { RowRefusal } from '../controls';
import { TargetEvents } from './TargetEvents';
import { TargetHealth } from './TargetHealth';
import { TargetProbe } from './TargetProbe';

/**
 * TargetRow shows one target, collapsed to its name and host, expanded to the
 * whole request. The address, headers and body are committed on blur, because
 * the server refuses the whole settings document when one row is invalid and
 * the autosave would fire mid-typing. A refused value stays on screen, marked,
 * while the collapsed row shows what is stored.
 */
export function TargetRow({
  row,
  index,
  last,
  stored,
  open,
  triggers,
  placeholders,
  status,
  onToggle,
  onChange,
  onCommit,
  onRemove,
}: {
  row: EventTargetRow;
  index: number;
  last: boolean;
  /** Already in the shared draft, so the server can report its health. */
  stored: boolean;
  open: boolean;
  /** From GET /api/scripts/triggers. */
  triggers: string[];
  /** From GET /api/eventtargets/placeholders. */
  placeholders: Placeholder[];
  status?: EventTargetStatus;
  onToggle: () => void;
  onChange: (next: EventTargetRow) => void;
  /** For a row not in the draft yet: called once its address is valid. */
  onCommit?: (next: EventTargetRow) => void;
  onRemove: () => void;
}) {
  const { t } = useT();

  const [urlText, setUrlText] = useState(row.url);
  const [urlBad, setUrlBad] = useState(false);
  const [headersText, setHeadersText] = useState(() => formatHeaders(row.headers));
  const [headersBad, setHeadersBad] = useState(false);
  const [bodyText, setBodyText] = useState(row.body ?? '');
  const [bodyBad, setBodyBad] = useState(false);

  // The save answer replaces the draft, and the server may have trimmed a
  // value or dropped a header because the address moved, so follow it.
  useEffect(() => {
    setUrlText(row.url);
    setUrlBad(false);
  }, [row.url]);
  useEffect(() => {
    setHeadersText(formatHeaders(row.headers));
    setHeadersBad(false);
  }, [row.headers]);
  useEffect(() => {
    setBodyText(row.body ?? '');
    setBodyBad(false);
  }, [row.body]);

  const commitUrl = () => {
    const url = urlText.trim();
    if (url === row.url) {
      setUrlBad(false);
      return;
    }
    if (!usableAddress(url)) {
      setUrlBad(true);
      // An unsaved row keeps the half-typed address; a stored row's draft is
      // left alone so the save is not refused.
      if (!stored) onChange({ ...row, url });
      return;
    }
    setUrlBad(false);
    if (stored) onChange({ ...row, url });
    else if (onCommit) onCommit({ ...row, url });
  };

  const commitHeaders = () => {
    const parsed = parseHeaders(headersText);
    // A bad line refuses the whole block, since a dropped header is a token
    // that silently stops being sent.
    if (parsed === null || Object.values(parsed).some((v) => !balancedTemplate(v))) {
      setHeadersBad(true);
      return;
    }
    setHeadersBad(false);
    const next = { ...row };
    if (Object.keys(parsed).length === 0) delete next.headers;
    else next.headers = parsed;
    if (formatHeaders(next.headers) === formatHeaders(row.headers)) return;
    onChange(next);
  };

  const commitBody = () => {
    if (bodyText === (row.body ?? '')) {
      setBodyBad(false);
      return;
    }
    if (!balancedTemplate(bodyText)) {
      setBodyBad(true);
      return;
    }
    setBodyBad(false);
    const next = { ...row };
    if (bodyText === '') delete next.body;
    else next.body = bodyText;
    onChange(next);
  };

  // The server's reading of the host where it has one, so the row and the
  // status table agree.
  const host = status?.host || hostOf(urlText);
  const ticked = row.triggers?.length ?? 0;
  const method: TargetMethod = row.method ?? 'POST';

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="grid grid-cols-[1fr_auto] items-center gap-3 py-2.5">
        <button type="button" onClick={onToggle} aria-expanded={open} className="flex min-w-0 items-center gap-3 text-start">
          <span className="glim-num w-5 shrink-0 text-xs text-carbon-textMuted">{index + 1}</span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm text-carbon-text">
              {row.name.trim() || <span className="text-carbon-textMuted">{t('settings.eventTargets.name')}</span>}
            </span>
            {/* Only the host, since ntfy and Gotify take their credential in
                the query. */}
            <span dir="ltr" className="block truncate text-[11px] text-carbon-textMuted">
              {host}
            </span>
          </span>
          {ticked > 0 && <TickedCount n={ticked} />}
          {/* Only the off state is marked. */}
          {!row.enabled && (
            <span className="hidden shrink-0 text-[11px] uppercase tracking-wider text-carbon-textMuted sm:block">
              {t('settings.modules.off')}
            </span>
          )}
        </button>
        <div className="flex items-center gap-1.5">
          <IconBadge
            // A lone glyph takes half its 32px badge.
            icon={<IconTrash width={16} height={16} />}
            hue={index}
            title={t('settings.eventTargets.remove')}
            aria-label={t('settings.eventTargets.remove')}
            // No blur, so no commit happens in front of the removal.
            onMouseDown={(e) => e.preventDefault()}
            onClick={onRemove}
          />
        </div>
      </div>
      {stored && <RowRefusal field={`eventTargets.${index}`} />}

      {open && (
        <div className="glim-well mb-3 flex flex-col gap-4 p-4">
          <ToggleRow
            label={t('settings.eventTargets.enabled')}
            hint={t('settings.eventTargets.enabledHint')}
            checked={row.enabled}
            onChange={(v) => onChange({ ...row, enabled: v })}
            hue={index}
          />

          <Field label={t('settings.eventTargets.name')} hint={t('settings.eventTargets.nameHint')}>
            <TextInput value={row.name} onChange={(e) => onChange({ ...row, name: e.target.value })} />
          </Field>

          <Field label={t('settings.eventTargets.url')} hint={t('settings.eventTargets.urlHint')}>
            <TextInput
              dir="ltr"
              spellCheck={false}
              value={urlText}
              placeholder="https://ntfy.example/my-topic"
              // Marked, not corrected; the focus ring covers the halo while typing.
              aria-invalid={urlBad}
              className={urlBad ? 'shadow-[0_0_0_2px_var(--status-warn-text)]' : ''}
              onChange={(e) => {
                setUrlText(e.target.value);
                setUrlBad(false);
              }}
              onBlur={commitUrl}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  e.currentTarget.blur();
                }
              }}
            />
          </Field>
          {host !== '' && (
            <p className="text-xs text-carbon-textMuted">{t('settings.eventTargets.leavesTheBox', { host })}</p>
          )}

          {/* FieldGroup, because a Field's label would pass a click on the
              caption to GET. */}
          <FieldGroup label={t('settings.eventTargets.method')} hint={t('settings.eventTargets.methodHint')}>
            <Tabs
              size="sm"
              label={t('settings.eventTargets.method')}
              active={method}
              onSelect={(id) => onChange({ ...row, method: id as TargetMethod })}
              items={[
                { id: 'POST', label: 'POST' },
                { id: 'PUT', label: 'PUT' },
                { id: 'GET', label: 'GET' },
              ]}
            />
          </FieldGroup>

          <Field label={t('settings.eventTargets.headers')} hint={t('settings.eventTargets.headersHint')}>
            {/* The halo sits on a wrapper because TextArea's className prop
                would replace its own styling. */}
            <div className={headersBad ? 'rounded-[var(--radius-control)] shadow-[0_0_0_2px_var(--status-warn-text)]' : ''}>
              <TextArea
                dir="ltr"
                spellCheck={false}
                rows={3}
                value={headersText}
                placeholder={'Authorization: Bearer ...\nContent-Type: text/plain'}
                aria-invalid={headersBad}
                onChange={(e) => {
                  setHeadersText(e.target.value);
                  setHeadersBad(false);
                }}
                onBlur={commitHeaders}
              />
            </div>
          </Field>

          {/* GET sends no body. */}
          {method !== 'GET' && (
            <Field label={t('settings.eventTargets.body')} hint={t('settings.eventTargets.bodyHint')}>
              <div className={bodyBad ? 'rounded-[var(--radius-control)] shadow-[0_0_0_2px_var(--status-warn-text)]' : ''}>
                <TextArea
                  dir="ltr"
                  spellCheck={false}
                  rows={4}
                  value={bodyText}
                  placeholder={'%%task.name%%'}
                  aria-invalid={bodyBad}
                  onChange={(e) => {
                    setBodyText(e.target.value);
                    setBodyBad(false);
                  }}
                  onBlur={commitBody}
                />
              </div>
            </Field>
          )}

          <TargetEvents
            triggers={triggers}
            picked={row.triggers ?? []}
            hue={index}
            onChange={(next) => {
              const out = { ...row };
              if (next.length === 0) delete out.triggers;
              else out.triggers = next;
              onChange(out);
            }}
          />

          <Placeholders list={placeholders} picked={row.triggers ?? []} />

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            {/* 0 takes the server's default of three, which is shown beside
                the box. */}
            <Field label={t('settings.eventTargets.attempts')} hint={t('settings.eventTargets.attemptsHint')}>
              <div className="flex items-center gap-2">
                <NumberInput
                  dir="ltr"
                  value={row.attempts}
                  min={0}
                  max={MAX_ATTEMPTS}
                  step={1}
                  onValue={(v) => onChange({ ...row, attempts: clamp(v, MAX_ATTEMPTS) })}
                />
                <span className="shrink-0 text-[11px] text-carbon-textMuted">
                  {row.attempts === 0
                    ? t('settings.eventTargets.attemptsDefault')
                    : String(Math.min(row.attempts, MAX_ATTEMPTS))}
                </span>
              </div>
            </Field>
            <Field label={t('settings.eventTargets.timeout')} hint={t('settings.eventTargets.timeoutHint')}>
              <div className="flex items-center gap-2">
                <NumberInput
                  dir="ltr"
                  value={row.timeoutSeconds}
                  min={0}
                  max={MAX_TIMEOUT_SECONDS}
                  step={1}
                  onValue={(v) => onChange({ ...row, timeoutSeconds: clamp(v, MAX_TIMEOUT_SECONDS) })}
                />
                <span className="glim-num shrink-0 text-[11px] text-carbon-textMuted">
                  {row.timeoutSeconds === 0 ? DEFAULT_TIMEOUT_SECONDS : Math.min(row.timeoutSeconds, MAX_TIMEOUT_SECONDS)}
                </span>
              </div>
            </Field>
          </div>

          {stored && <TargetHealth status={status} />}
          <TargetProbe row={{ ...row, url: urlText.trim() }} />
        </div>
      )}
    </li>
  );
}

/**
 * clamp stores a number the way notify.Sanitize would, keeping 0, so the box
 * does not change once the save comes back.
 */
function clamp(v: number, max: number): number {
  if (!Number.isFinite(v)) return 0;
  const whole = Math.round(v);
  if (whole <= 0) return 0;
  return Math.min(whole, max);
}

/** TickedCount shows how many events are ticked, with the house tooltip. */
function TickedCount({ n }: { n: number }) {
  const { t } = useT();
  const tip = useTooltip<HTMLSpanElement>(t('settings.eventTargets.events'));
  // The span sits inside the row's expand button, so it takes no role and no
  // tab stop of its own.
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <span {...tipHoverProps} className="glim-num hidden shrink-0 text-xs text-carbon-textSub sm:block">
        {n}
      </span>
      {tip.node}
    </>
  );
}

/** PlaceholderChip is its own component because the tooltip is a hook. */
function PlaceholderChip({ name, tip: tipText, unused }: { name: string; tip: string; unused: boolean }) {
  const tip = useTooltip<HTMLElement>(tipText);
  return (
    <>
      <code
        dir="ltr"
        {...tip.triggerProps}
        className={`rounded-[var(--radius-pill)] bg-carbon-surface2 px-1.5 py-0.5 text-[11px] ${
          unused ? 'text-carbon-textMuted line-through' : 'text-carbon-textSub'
        }`}
      >
        {name}
      </code>
      {tip.node}
    </>
  );
}

/**
 * Placeholders lists the names the server fills into the address, a header or
 * the body, and marks the ones no ticked event carries, which expand to nothing.
 */
function Placeholders({ list, picked }: { list: Placeholder[]; picked: string[] }) {
  const { t } = useT();
  if (list.length === 0) return null;
  return (
    <FieldGroup label={t('settings.eventTargets.placeholders')} hint={t('settings.eventTargets.placeholdersHint')}>
      <div className="flex flex-wrap gap-1.5">
        {list.map((p) => {
          // Empty triggers means every event carries it. Nothing ticked is
          // reported by the events picker instead.
          const unused =
            picked.length > 0 && p.triggers !== undefined && p.triggers.length > 0 && !p.triggers.some((tr) => picked.includes(tr));
          const name = `%%${p.name}%%`;
          return (
            <PlaceholderChip
              key={p.name}
              name={name}
              unused={unused}
              tip={unused ? t('settings.eventTargets.placeholderUnused', { name }) : p.scope}
            />
          );
        })}
      </div>
    </FieldGroup>
  );
}

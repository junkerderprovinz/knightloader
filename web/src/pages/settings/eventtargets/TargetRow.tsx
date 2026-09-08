import { useEffect, useState } from 'react';
import { Field, FieldGroup, IconBadge, NumberInput, TextArea, TextInput, ToggleRow } from '../../../components/ui';
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
import { TargetEvents } from './TargetEvents';
import { TargetHealth } from './TargetHealth';
import { TargetProbe } from './TargetProbe';

/**
 * One target, collapsed to what it is called and where it reports to, expanded
 * to the whole request.
 *
 * THREE FIELDS ARE TYPED HERE AND COMMITTED ON THE WAY OUT, and the reason is
 * the same one for all three: the settings shell autosaves 600 ms after any
 * draft change, and the server refuses the ENTIRE settings document when one row
 * will not validate. "https" on its way to "https://ntfy.example/x", a header
 * line on its way to having a colon, and "%%task" on its way to "%%task.name%%"
 * would each fire a save, be refused, and take every unrelated edit on every
 * other settings page with them. Everything else on this row - the name, the
 * method, the switch, the events, the two numbers - cannot be typed into an
 * invalid state at all, and writes straight through.
 *
 * A REFUSED VALUE IS MARKED AND KEPT, NEVER CORRECTED. What was typed stays on
 * screen with the halo on it, and the collapsed row above goes on showing what
 * is really stored - which is the honest difference between the two. Correcting
 * it would be this page deciding what somebody meant.
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
  /** Already in the shared draft, and therefore already a target the server
   *  knows about and can report health for. */
  stored: boolean;
  open: boolean;
  /** From GET /api/scripts/triggers - the registry that actually fires them. */
  triggers: string[];
  /** From GET /api/eventtargets/placeholders - the expander's own table. */
  placeholders: Placeholder[];
  status?: EventTargetStatus;
  onToggle: () => void;
  onChange: (next: EventTargetRow) => void;
  /** Set only for a row that is not in the draft yet: called the moment its
   *  address is one the server will take. */
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

  // The save answer REPLACES the draft, so a value the server trimmed, or a
  // header value it dropped because the address moved, comes back spelled
  // differently from what was typed. Follow it rather than holding the old text
  // on screen: what came back is what this target now does, and the dropped
  // header is the single most important thing on this row to see happen.
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
      // Kept even though it cannot be stored, so collapsing a half-typed row
      // does not throw away what was typed into it. Safe for an unsaved row
      // because nothing validates it there; for a stored one the draft is left
      // alone, which is what keeps the save from being refused.
      if (!stored) onChange({ ...row, url });
      return;
    }
    setUrlBad(false);
    if (stored) onChange({ ...row, url });
    else if (onCommit) onCommit({ ...row, url });
  };

  const commitHeaders = () => {
    const parsed = parseHeaders(headersText);
    // Null and not "skip the bad line": a header quietly dropped is a token that
    // quietly stops being sent, and the far end then answers 401 with nothing
    // here to explain it.
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

  // The host the SERVER read out of the address where there is one, so the row
  // and the status table cannot disagree; the locally parsed one for a target
  // that has never been saved, which is the only case the server has nothing to
  // say about.
  const host = status?.host || hostOf(urlText);
  const ticked = row.triggers?.length ?? 0;
  const method: TargetMethod = row.method ?? 'POST';

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="group grid grid-cols-[1fr_auto] items-center gap-3 py-2.5">
        <button type="button" onClick={onToggle} aria-expanded={open} className="flex min-w-0 items-center gap-3 text-left">
          <span className="glim-num w-5 shrink-0 text-xs text-carbon-textMuted">{index + 1}</span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm text-carbon-text">
              {row.name.trim() || <span className="text-carbon-textMuted">{t('settings.eventTargets.name')}</span>}
            </span>
            {/* The HOST and never the whole address: ntfy and Gotify both take
                their credential in the query, and a collapsed row is the one
                thing on this page somebody screenshots. dir=ltr because a host
                is never read right to left whatever the interface language is. */}
            <span dir="ltr" className="block truncate text-[11px] text-carbon-textMuted">
              {host}
            </span>
          </span>
          {/* What this row would do, in two words: how many events, and whether
              it is allowed to act on them. Both are needed - a target with six
              events ticked and its switch off sends nothing. */}
          {ticked > 0 && (
            <span className="glim-num hidden shrink-0 text-xs text-carbon-textSub sm:block" title={t('settings.eventTargets.events')}>
              {ticked}
            </span>
          )}
          {/* Only the OFF state is marked. A badge on every switched-on row is a
              badge nobody reads, and the thing worth spotting in a list of six
              targets is the one that is not sending. */}
          {!row.enabled && (
            <span className="hidden shrink-0 text-[10px] uppercase tracking-wider text-carbon-textMuted sm:block">
              {t('settings.modules.off')}
            </span>
          )}
        </button>
        {/* The one row action, on hover and on keyboard focus, so a long list
            reads as content rather than as a wall of buttons. */}
        <div className="flex items-center gap-1.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          <IconBadge
            kind="danger"
            icon={<IconTrash width={14} height={14} />}
            hue={index}
            title={t('settings.eventTargets.remove')}
            aria-label={t('settings.eventTargets.remove')}
            // Keeping focus in the address box means no blur, and therefore no
            // commit, in front of this click. Without it a new row whose address
            // was just typed would be written into the draft on the way out and
            // this press would then remove a row that no longer exists.
            onMouseDown={(e) => e.preventDefault()}
            onClick={onRemove}
          />
        </div>
      </div>

      {open && (
        <div className="glim-well mb-3 flex flex-col gap-4 p-4">
          {/* A ToggleRow and never a checkbox, and first on the row because it
              is the only control here that changes what the instance does. */}
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
              // aria-invalid and no commit, rather than a corrected value. The
              // halo is the same shape the focus ring uses and loses to it while
              // the box is focused, which is where a correction is being made
              // anyway.
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
          {/* Said here rather than only in the bubble above: this is the one
              sentence on the page that names what the feature actually does with
              the machine it runs on. */}
          {host !== '' && (
            <p className="text-xs text-carbon-textMuted">{t('settings.eventTargets.leavesTheBox', { host })}</p>
          )}

          {/* A FieldGroup and not a Field: a Field is a <label>, and a label
              hands a click on its caption to the first control inside it, which
              here would silently pick GET. */}
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
            {/* The halo sits on a wrapper and not on the control: ui.tsx's
                TextArea sets its own className and spreads the caller's props
                AFTER it, so a className passed in here would replace the input
                styling outright rather than add to it. */}
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

          {/* GET sends no body, so the template is left out rather than shown
              and quietly ignored - a field that does nothing is a field somebody
              spends ten minutes on. */}
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
            {/* 0 is a value here and not a blank: it means this row has no
                opinion and takes the server's three. min is therefore 0 and not
                1, and the number in force is shown beside the box so the two
                readings are never confused. */}
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

          {/* Only for a stored row: a target the server has never seen has no
              health, and every line of it would read as a fault on something
              that does not exist yet. */}
          {stored && <TargetHealth status={status} />}
          <TargetProbe row={{ ...row, url: urlText.trim() }} />
        </div>
      )}
    </li>
  );
}

/** The number as notify.Sanitize would store it: 0 passes through untouched
 *  because it is a value and not a blank, and anything else lands inside the
 *  band the server would silently pull it into anyway - so the box does not
 *  change under the cursor once the save comes back. */
function clamp(v: number, max: number): number {
  if (!Number.isFinite(v)) return 0;
  const whole = Math.round(v);
  if (whole <= 0) return 0;
  return Math.min(whole, max);
}

/**
 * The names that can be written into the address, a header or the body.
 *
 * From the server, so this list is what this build really fills in. The one
 * thing it adds on top of the names is the warning that matters: a placeholder
 * whose payload none of the ticked events carries is not an error, it simply
 * expands to nothing, and a message that arrives with a blank where the file
 * name should be is the hardest kind of mistake to trace back to this page.
 */
function Placeholders({ list, picked }: { list: Placeholder[]; picked: string[] }) {
  const { t } = useT();
  if (list.length === 0) return null;
  return (
    <FieldGroup label={t('settings.eventTargets.placeholders')} hint={t('settings.eventTargets.placeholdersHint')}>
      <div className="flex flex-wrap gap-1.5">
        {list.map((p) => {
          // Empty triggers means every trigger carries it, so it is never
          // unused. Nothing ticked at all is its own state and is reported by
          // the events picker, not repeated here for forty names.
          const unused =
            picked.length > 0 && p.triggers !== undefined && p.triggers.length > 0 && !p.triggers.some((tr) => picked.includes(tr));
          const name = `%%${p.name}%%`;
          return (
            <code
              key={p.name}
              dir="ltr"
              title={unused ? t('settings.eventTargets.placeholderUnused', { name }) : p.scope}
              className={`rounded-[var(--radius-control)] bg-carbon-surface2 px-1.5 py-0.5 text-[11px] ${
                unused ? 'text-carbon-textMuted line-through' : 'text-carbon-textSub'
              }`}
            >
              {name}
            </code>
          );
        })}
      </div>
    </FieldGroup>
  );
}

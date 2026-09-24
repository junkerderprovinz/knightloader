import { useEffect, useState } from 'react';
import {
  Button,
  Card,
  Field,
  FieldGroup,
  IconBadge,
  Modal,
  NumberInput,
  SectionTitle,
  TextInput,
  useTooltip,
} from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { IconClose, IconPlus, IconTrash } from '../../../lib/icons';
import { useT, type TranslationKey } from '../../../lib/i18n';
import {
  deleteMediaHook,
  fetchMediaHooks,
  fetchOptions,
  REDACTED_HEADER,
  saveMediaHook,
  testMediaHook,
  type MediaHook,
  type MediaHookResult,
} from '../../../lib/api';

// Media hooks are the addresses called once a package's files are in place,
// usually a media library told to rescan. The header value is sealed and never
// comes back, so a stored hook opens with REDACTED_HEADER, which the server
// reads as "keep", as in HeaderProfiles.tsx. A hook saves through its own route
// with its sealed value in one request, outside the settings draft. Categories
// refer to a hook by id, so the id is read only once stored. The host is shown
// under the address, with a note when it is not on a private range.

/** The internal/mediahook failure codes; an unknown one shows the raw error. */
const PROBLEM_KEYS: Record<string, TranslationKey> = {
  dns: 'settings.mediahook.problem.dns',
  refused: 'settings.mediahook.problem.refused',
  timeout: 'settings.mediahook.problem.timeout',
  tls: 'settings.mediahook.problem.tls',
  auth: 'settings.mediahook.problem.auth',
  notFound: 'settings.mediahook.problem.notFound',
  method: 'settings.mediahook.problem.method',
  redirect: 'settings.mediahook.problem.redirect',
  server: 'settings.mediahook.problem.server',
  proxy: 'settings.mediahook.problem.proxy',
  unknown: 'settings.mediahook.problem.unknown',
};

interface Draft {
  /** Empty for a new hook, the only state in which the id can be edited. */
  original: string;
  id: string;
  name: string;
  url: string;
  method: string;
  headerName: string;
  headerValue: string;
  /** True while the value box still holds the placeholder. */
  stored: boolean;
  waitSeconds: number;
}

function draftFor(h: MediaHook): Draft {
  return {
    original: h.id,
    id: h.id,
    name: h.name ?? '',
    url: h.url,
    method: h.method,
    headerName: h.headerName ?? '',
    // Never '' for a stored value, which would clear it.
    headerValue: h.hasValue ? REDACTED_HEADER : '',
    stored: h.hasValue,
    waitSeconds: h.waitSeconds,
  };
}

const emptyDraft = (method: string): Draft => ({
  original: '',
  id: '',
  name: '',
  url: '',
  method,
  headerName: '',
  headerValue: '',
  stored: false,
  waitSeconds: 60,
});

export function MediaHooksCard({ hue }: { hue: number }) {
  const { t } = useT();
  const [hooks, setHooks] = useState<MediaHook[]>([]);
  const [methods, setMethods] = useState<string[]>([]);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [busy, setBusy] = useState(false);
  const [testing, setTesting] = useState('');
  const [error, setError] = useState('');
  // The hook whose removal is being confirmed.
  const [confirming, setConfirming] = useState<MediaHook | null>(null);

  useEffect(() => {
    let alive = true;
    void fetchMediaHooks().then(
      (list) => {
        if (alive) setHooks(list);
      },
      () => {
        /* Add still works, and a save answers with the real listing. */
      },
    );
    // The server offers only the methods this build can send.
    void fetchOptions().then(
      (o) => {
        if (alive) setMethods(o.mediaHookMethods ?? []);
      },
      () => {
        /* the strip stays out rather than offering a guess at the methods */
      },
    );
    return () => {
      alive = false;
    };
  }, []);

  const run = async (work: () => Promise<MediaHook[] | void>) => {
    setBusy(true);
    setError('');
    try {
      const next = await work();
      if (next) setHooks(next);
      else setHooks(await fetchMediaHooks());
      setDraft(null);
    } catch (e) {
      // The server's sentence names the field and what to send.
      setError(String(e).replace(/^(Error|ApiError):\s*/, ''));
    } finally {
      setBusy(false);
    }
  };

  const add = () => {
    setError('');
    setDraft(emptyDraft(methods[0] ?? 'GET'));
  };

  const save = () => {
    if (!draft) return;
    void run(() =>
      saveMediaHook({
        id: draft.id.trim(),
        name: draft.name.trim(),
        url: draft.url.trim(),
        method: draft.method,
        headerName: draft.headerName.trim(),
        // The placeholder means "keep"; an empty box was cleared by hand.
        headerValue: draft.headerValue,
        waitSeconds: draft.waitSeconds,
      }),
    );
  };

  const test = (h: MediaHook) => {
    setTesting(h.id);
    setError('');
    void testMediaHook(h.id)
      .then(
        () => fetchMediaHooks().then(setHooks),
        (e: unknown) => setError(String(e).replace(/^(Error|ApiError):\s*/, '')),
      )
      .finally(() => setTesting(''));
  };

  // The last call is kept in memory, so null means none since the restart.
  const lastLine = (last: MediaHookResult | null): string => {
    if (!last) return t('settings.mediahook.lastCallNever');
    const ms = last.durationMs;
    if (last.ok) return t('settings.mediahook.lastCallOk', { status: last.status ?? 0, ms });
    const key = PROBLEM_KEYS[last.code ?? ''] ?? PROBLEM_KEYS.unknown;
    const params = { ...(last.params ?? {}), error: last.error ?? '' };
    return `${t('settings.mediahook.lastCallFailed', { ms })} ${t(key, params)}`;
  };

  // The parsed host on its own shows a missing port or a misplaced path.
  const hostOf = (url: string): string => {
    try {
      return new URL(url.trim()).host;
    } catch {
      return '';
    }
  };

  const lastFor = (last: MediaHookResult | null): string => {
    if (!last) return '';
    if (last.test) return t('settings.mediahook.lastCallTest');
    if ((last.packages ?? 0) > 1) return t('settings.mediahook.lastCallCoalesced', { n: last.packages ?? 0 });
    if (last.package) return t('settings.mediahook.lastCallFor', { name: last.package });
    return '';
  };

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle
        hint={t('settings.mediahook.hint')}
        right={
          <Button icon={<IconPlus width={16} height={16} />} disabled={busy || draft !== null} onClick={add}>
            {t('settings.mediahook.add')}
          </Button>
        }
      >
        {t('settings.mediahook.title')}
      </SectionTitle>

      {hooks.length === 0 && !draft ? (
        // Inside the card rather than an EmptyState, which would hide Add.
        <p className="py-6 text-center text-sm text-carbon-textSub">
          {t('settings.mediahook.empty')}
          <span className="mt-1 block text-[11px] text-carbon-textMuted">{t('settings.mediahook.emptyHint')}</span>
        </p>
      ) : (
        <ul className="flex flex-col">
          {hooks.map((h, i) => (
            <li
              key={h.id}
              className={`flex flex-col gap-1 py-2.5 ${
                i === hooks.length - 1 && !draft ? '' : 'border-b border-carbon-border/60'
              }`}
            >
              <div className="flex items-center gap-3">
                <button
                  type="button"
                  className="flex min-w-0 flex-1 items-center gap-3 text-left"
                  onClick={() => {
                    setError('');
                    setDraft(draftFor(h));
                  }}
                >
                  <span className="shrink-0 text-sm text-carbon-text">{h.name || h.id}</span>
                  <HostLine host={h.host} url={h.url} />
                  <span className="shrink-0 text-[11px] text-carbon-textMuted">
                    {h.usedBy.length > 0
                      ? t('settings.mediahook.usedBy', { n: h.usedBy.length })
                      : t('settings.mediahook.usedByNone')}
                  </span>
                </button>
                <Button
                  kind="ghost"
                  disabled={busy || testing !== ''}
                  onClick={() => test(h)}
                >
                  {testing === h.id ? t('settings.mediahook.testRunning') : t('settings.mediahook.test')}
                </Button>
                <IconBadge
                  // A lone glyph takes half its 32px badge.
                  icon={<IconTrash width={16} height={16} />}
                  hue={i}
                  title={t('settings.mediahook.delete')}
                  aria-label={`${t('settings.mediahook.delete')} · ${h.name || h.id}`}
                  disabled={busy}
                  onClick={() => {
                    // Refused here too, so the reason is translated.
                    if (h.usedBy.length > 0) {
                      setError(t('settings.mediahook.deleteInUse', { name: h.name || h.id, n: h.usedBy.length }));
                      return;
                    }
                    // Confirmed, since the sealed value cannot be put back.
                    setConfirming(h);
                  }}
                />
              </div>
              <p className="ps-0 text-[11px] text-carbon-textMuted">
                {lastLine(h.last)}
                {lastFor(h.last) !== '' && <span className="ms-1">{lastFor(h.last)}</span>}
              </p>
              {!h.private && (
                <p className="text-[11px] text-statusWarn">{t('settings.mediahook.goesToForeign')}</p>
              )}
            </li>
          ))}
        </ul>
      )}

      {draft && (
        <div className="glim-well flex flex-col gap-4 p-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t('settings.mediahook.name')} hint={t('settings.mediahook.nameHint')}>
              <TextInput
                dir="ltr"
                spellCheck={false}
                value={draft.id}
                placeholder="jellyfin"
                // Read only once stored, since categories refer to it.
                readOnly={draft.original !== ''}
                className={draft.original !== '' ? 'cursor-default opacity-70' : ''}
                onChange={(e) => setDraft({ ...draft, id: e.target.value })}
              />
            </Field>
            {/* The (i) explains what 0 means while it is set. */}
            <Field
              label={t('settings.mediahook.wait')}
              hint={
                draft.waitSeconds === 0
                  ? `${t('settings.mediahook.waitHint')} ${t('settings.mediahook.waitImmediate')}`
                  : t('settings.mediahook.waitHint')
              }
            >
              <NumberInput
                value={draft.waitSeconds}
                min={0}
                max={3600}
                step={30}
                onValue={(v) => setDraft({ ...draft, waitSeconds: Math.max(0, v) })}
              />
            </Field>
          </div>

          <Field label={t('settings.mediahook.url')} hint={t('settings.mediahook.urlHint')}>
            <TextInput
              dir="ltr"
              spellCheck={false}
              value={draft.url}
              placeholder="http://jellyfin.lan:8096/Library/Refresh"
              onChange={(e) => setDraft({ ...draft, url: e.target.value })}
            />
          </Field>

          {hostOf(draft.url) !== '' && (
            <p className="-mt-2 text-[11px] text-carbon-textMuted">
              {t('settings.mediahook.goesTo', { host: hostOf(draft.url) })}
            </p>
          )}

          {methods.length > 0 && (
            <FieldGroup label={t('settings.mediahook.method')} hint={t('settings.mediahook.methodHint')}>
              {/* The strip wraps, so no scroller goes around it. */}
              <Tabs
                variant="well"
                size="sm"
                label={t('settings.mediahook.method')}
                active={draft.method}
                onSelect={(id) => setDraft({ ...draft, method: id })}
                items={methods.map((m) => ({ id: m, label: m }))}
              />
            </FieldGroup>
          )}

          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t('settings.mediahook.headerName')} hint={t('settings.mediahook.headerNameHint')}>
              <TextInput
                dir="ltr"
                spellCheck={false}
                value={draft.headerName}
                placeholder="X-Emby-Token"
                onChange={(e) => setDraft({ ...draft, headerName: e.target.value })}
              />
            </Field>
            <Field
              label={draft.stored ? t('settings.mediahook.valueStored') : t('settings.mediahook.headerValue')}
              hint={draft.stored ? t('settings.mediahook.valueStoredHint') : t('settings.mediahook.headerValueHint')}
            >
              <TextInput
                dir="ltr"
                spellCheck={false}
                value={draft.headerValue}
                // Typing turns the kept value into a real one.
                onChange={(e) => setDraft({ ...draft, headerValue: e.target.value, stored: false })}
              />
            </Field>
          </div>

          {draft.original !== '' && (
            <FieldGroup label={t('settings.mediahook.lastCall')} hint={t('settings.mediahook.lastCallHint')}>
              <p className="text-xs text-carbon-textSub">
                {lastLine(hooks.find((h) => h.id === draft.original)?.last ?? null)}
              </p>
            </FieldGroup>
          )}

          {/* The spacer and the error come first so Save ends the row. The JSX
              order sets it, so the row mirrors in right-to-left languages. */}
          <div className="flex items-center gap-3">
            <span className="flex-1" />
            {error && <p className="text-xs text-statusWarn">{error}</p>}
            <Button kind="ghost" disabled={busy} onClick={() => setDraft(null)}>
              {t('common.cancel')}
            </Button>
            <Button disabled={busy || draft.id.trim() === '' || draft.url.trim() === ''} onClick={save}>
              {t('settings.mediahook.save')}
            </Button>
          </div>
        </div>
      )}

      {!draft && error && <p className="text-xs text-statusWarn">{error}</p>}

      {confirming && (
        <Modal
          title={t('settings.mediahook.delete')}
          onClose={() => setConfirming(null)}
          footer={
            <>
              <span className="flex-1" />
              <Button
                kind="ghost"
                labelled
                icon={<IconClose />}
                title={t('common.cancel')}
                disabled={busy}
                onClick={() => setConfirming(null)}
              />
              <Button
                kind="secondary"
                disabled={busy}
                onClick={() => {
                  const id = confirming.id;
                  setConfirming(null);
                  void run(async () => {
                    await deleteMediaHook(id);
                  });
                }}
              >
                {t('settings.mediahook.delete')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-textSub">
            {t('settings.mediahook.deleteConfirm', { name: confirming.name || confirming.id })}
          </p>
        </Modal>
      )}
    </Card>
  );
}

/** HostLine is its own component because the tooltip is a hook. */
function HostLine({ host, url }: { host: string; url: string }) {
  const tip = useTooltip<HTMLSpanElement>(url);
  // The span sits inside the row's edit button, so it takes no role and no
  // tab stop of its own.
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <span dir="ltr" {...tipHoverProps} className="min-w-0 flex-1 truncate text-xs text-carbon-textSub">
        {host}
      </span>
      {tip.node}
    </>
  );
}

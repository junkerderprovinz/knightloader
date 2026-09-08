import { useEffect, useState } from 'react';
import { Button, Card, Field, FieldGroup, IconBadge, NumberInput, SectionTitle, TextInput } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { IconPlus, IconTrash } from '../../../lib/icons';
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

/**
 * The address KnightLoader calls once a package has finished and its files have
 * been moved into place: a media library told to rescan, in practice.
 *
 * Five things about this card are decisions rather than layout.
 *
 * THE VALUE NEVER COMES BACK, and the editing form is therefore the dangerous
 * part. The listing route answers whether a header value is stored and never
 * what it is, so a form that opened a stored address, drew an empty box and sent
 * back what it was given would DELETE the token it was opened to edit - empty
 * means "clear this" here as it does for every other secret in this app. This
 * card makes that impossible by construction rather than by remembering: opening
 * a stored address fills the value box with REDACTED_HEADER, which the server
 * reads as "keep what is stored", and the only way to get an empty value into it
 * is to clear it by hand. There is no code path that produces an empty value the
 * user did not type. Same trap, same fix, same reasoning as HeaderProfiles.tsx.
 *
 * IT DOES NOT RIDE THE SETTINGS DRAFT. The row and its sealed value are two
 * halves of one thing a person edits in one form, and only one of the two can
 * live in settings.json. So this card saves itself through its own route, like
 * the account cards, and the Save bar at the bottom of the page knows nothing
 * about it. That also keeps the two halves from landing seconds apart, which is
 * what a debounced draft save would have done.
 *
 * THE NAME IS THE KEY AND CANNOT CHANGE. A drawer under Categories points at
 * this address BY that name, so renaming it here would leave every drawer
 * pointing at an address that no longer answers, silently - the identical rule
 * the header profiles' own id follows, and the reason the box is read-only once
 * the address is stored.
 *
 * WHERE THE CALL GOES IS DRAWN, NOT BURIED. The host is printed under the
 * address, and an address that is not on a private range says out loud that the
 * call, and the header with it, leaves this machine. `private` is answered
 * without a DNS lookup, so a host NAME reads as "not private" - erring towards
 * saying it out loud, because the sentence that is withheld wrongly is the one
 * nobody can see.
 *
 * THE LAST CALL IS IN MEMORY ONLY. It is the answer to "did that work", it
 * counts test calls, and a restart clears it without changing anything about the
 * address itself. The hint says so, because a blank line there otherwise reads
 * as an address that has stopped working.
 */

/** The failure codes internal/mediahook can report, each with the sentence that
 *  says what to try next. A code this build has no key for falls back to the
 *  unknown sentence with the raw error, so a newer server never draws a blank. */
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

/** The row being edited, stored or brand new. */
interface Draft {
  /** Empty for an address that does not exist yet, which is what makes the name
   *  box editable exactly once. */
  original: string;
  id: string;
  name: string;
  url: string;
  method: string;
  headerName: string;
  headerValue: string;
  /** True while the value box still holds the placeholder rather than something
   *  somebody typed. It is what the caption and its hint switch on. */
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
    // REDACTED_HEADER and never '': this single line is the whole of the
    // promise in the doc comment above.
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

  useEffect(() => {
    let alive = true;
    void fetchMediaHooks().then(
      (list) => {
        if (alive) setHooks(list);
      },
      () => {
        /* An empty table rather than a claim that nothing is stored: the Add
           button still works and a save answers with the real listing. */
      },
    );
    // The menu comes from the server for the same reason every other fixed
    // choice on these pages does: a verb this build cannot send must never be
    // offered as a segment that does nothing when pressed.
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
      // The server's own sentence. It already names the field and says what to
      // send, and a key of ours here would be a vaguer second copy of it.
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
        // The placeholder IS the "keep it" instruction, so it goes back
        // untouched. An empty box is the user having cleared it on purpose.
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

  /** What the last call did, in one line. Null is not a failure: it is a server
   *  that has not been restarted long enough to have called anything. */
  const lastLine = (last: MediaHookResult | null): string => {
    if (!last) return t('settings.mediahook.lastCallNever');
    const ms = last.durationMs;
    if (last.ok) return t('settings.mediahook.lastCallOk', { status: last.status ?? 0, ms });
    const key = PROBLEM_KEYS[last.code ?? ''] ?? PROBLEM_KEYS.unknown;
    const params = { ...(last.params ?? {}), error: last.error ?? '' };
    return `${t('settings.mediahook.lastCallFailed', { ms })} ${t(key, params)}`;
  };

  /** The host a typed address will actually be called on, or '' while it is not
   *  an address yet. Parsed rather than shown as typed, because what catches a
   *  mistake here is seeing the host on its own: a missing port and a path
   *  written where the host should be both disappear into a long line. */
  const hostOf = (url: string): string => {
    try {
      return new URL(url.trim()).host;
    } catch {
      return '';
    }
  };

  /** Which packages that call was for, or that somebody pressed the button. */
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
        // Inside the card, not instead of it: Add is the way out of this state
        // and an EmptyState would take the button off the page with it.
        <p className="py-6 text-center text-sm text-carbon-textSub">
          {t('settings.mediahook.empty')}
          <span className="mt-1 block text-[11px] text-carbon-textMuted">{t('settings.mediahook.emptyHint')}</span>
        </p>
      ) : (
        <ul className="flex flex-col">
          {hooks.map((h, i) => (
            <li
              key={h.id}
              className={`group flex flex-col gap-1 py-2.5 ${
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
                  <span dir="ltr" title={h.url} className="min-w-0 flex-1 truncate text-xs text-carbon-textSub">
                    {h.host}
                  </span>
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
                  kind="danger"
                  icon={<IconTrash width={14} height={14} />}
                  hue={i}
                  title={t('settings.mediahook.delete')}
                  aria-label={`${t('settings.mediahook.delete')} · ${h.name || h.id}`}
                  disabled={busy}
                  className="opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100"
                  onClick={() => {
                    // Answered here rather than by the server's 409, although
                    // both refuse it. The listing already carries the drawers,
                    // so the sentence can be in the reader's own language and
                    // can name what to do about it, which the server's English
                    // one cannot.
                    if (h.usedBy.length > 0) {
                      setError(t('settings.mediahook.deleteInUse', { name: h.name || h.id, n: h.usedBy.length }));
                      return;
                    }
                    // Confirmed, because it takes the sealed header value with
                    // it and there is nothing on this page to put it back from:
                    // the value was never here to begin with.
                    if (!window.confirm(t('settings.mediahook.deleteConfirm', { name: h.name || h.id }))) return;
                    void run(async () => {
                      await deleteMediaHook(h.id);
                    });
                  }}
                />
              </div>
              <p className="ps-0 text-[11px] text-carbon-textMuted">
                {lastLine(h.last)}
                {lastFor(h.last) !== '' && <span className="ms-1">{lastFor(h.last)}</span>}
              </p>
              {/* Only where it is true, and only as a fact: an address on the
                  open internet is a perfectly reasonable thing to want, and this
                  says what it means rather than warning somebody off it. */}
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
                // Read only once stored: a drawer under Categories points at
                // this address by this name, so renaming it here would leave
                // every drawer pointing at nothing, silently.
                readOnly={draft.original !== ''}
                className={draft.original !== '' ? 'cursor-default opacity-70' : ''}
                onChange={(e) => setDraft({ ...draft, id: e.target.value })}
              />
            </Field>
            <Field label={t('settings.mediahook.wait')} hint={t('settings.mediahook.waitHint')}>
              <NumberInput
                value={draft.waitSeconds}
                min={0}
                max={3600}
                step={30}
                onValue={(v) => setDraft({ ...draft, waitSeconds: Math.max(0, v) })}
              />
            </Field>
          </div>

          {/* Zero is a real answer and not an unset field, so it says what it
              means rather than leaving a bare 0 to be read as "off". */}
          {draft.waitSeconds === 0 && (
            <p className="-mt-2 text-[11px] text-carbon-textMuted">{t('settings.mediahook.waitImmediate')}</p>
          )}

          <Field label={t('settings.mediahook.url')} hint={t('settings.mediahook.urlHint')}>
            <TextInput
              dir="ltr"
              spellCheck={false}
              value={draft.url}
              placeholder="http://jellyfin.lan:8096/Library/Refresh"
              onChange={(e) => setDraft({ ...draft, url: e.target.value })}
            />
          </Field>

          {/* The host on its own, under the address, before anything is saved.
              It is the line that catches a port left off and a path typed where
              the host belongs, both of which vanish into a long address. */}
          {hostOf(draft.url) !== '' && (
            <p className="-mt-2 text-[11px] text-carbon-textMuted">
              {t('settings.mediahook.goesTo', { host: hostOf(draft.url) })}
            </p>
          )}

          {methods.length > 0 && (
            <FieldGroup label={t('settings.mediahook.method')} hint={t('settings.mediahook.methodHint')}>
              <div className="overflow-x-auto">
                <Tabs
                  variant="well"
                  size="sm"
                  className="w-fit"
                  label={t('settings.mediahook.method')}
                  active={draft.method}
                  onSelect={(id) => setDraft({ ...draft, method: id })}
                  items={methods.map((m) => ({ id: m, label: m }))}
                />
              </div>
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
                // The moment somebody types, the box stops being "kept" and
                // becomes a real value. Without this the placeholder would go
                // back as a literal token the first time anybody corrected a
                // typo in it.
                onChange={(e) => setDraft({ ...draft, headerValue: e.target.value, stored: false })}
              />
            </Field>
          </div>

          {/* Only for an address that is already stored: there is nothing to
              report about one nobody has saved yet, and a caption over an empty
              line would read as a call that produced nothing. */}
          {draft.original !== '' && (
            <FieldGroup label={t('settings.mediahook.lastCall')} hint={t('settings.mediahook.lastCallHint')}>
              <p className="text-xs text-carbon-textSub">
                {lastLine(hooks.find((h) => h.id === draft.original)?.last ?? null)}
              </p>
            </FieldGroup>
          )}

          <div className="flex items-center gap-3">
            <Button disabled={busy || draft.id.trim() === '' || draft.url.trim() === ''} onClick={save}>
              {t('settings.mediahook.save')}
            </Button>
            <Button kind="ghost" disabled={busy} onClick={() => setDraft(null)}>
              {t('common.cancel')}
            </Button>
            {error && <p className="text-xs text-statusWarn">{error}</p>}
          </div>
        </div>
      )}

      {!draft && error && <p className="text-xs text-statusWarn">{error}</p>}
    </Card>
  );
}

import { useEffect, useState } from 'react';
import { Button, Card, Field, IconBadge, SectionTitle, TextInput } from '../../../components/ui';
import { IconPlus, IconTrash } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import {
  deleteHeaderProfile,
  fetchHeaderProfiles,
  REDACTED_HEADER,
  saveHeaderProfile,
  type HeaderProfile,
  type HeaderProfileLine,
} from '../../../lib/api';

/**
 * A person's own request headers for one site: the cookie from a signed-in
 * browser session, the Referer a forum insists on, the Basic auth a seedbox
 * sits behind.
 *
 * Four things about this card are decisions rather than layout.
 *
 * THE VALUES NEVER COME BACK. A profile is sealed in the credential store, and
 * the listing route answers its origin and the header NAMES it holds, nothing
 * else. That is deliberate and it is why this feature could not be reached from
 * anywhere for weeks: sealing it was the easy half.
 *
 * WHICH MAKES THE EDITING FORM THE DANGEROUS PART, and the reason for the
 * shape below. An empty value means "clear this header", here as it does for
 * every other secret in this app. So a form that opened a stored profile, drew
 * empty boxes for its headers and sent back what it was given would DELETE the
 * headers it was opened to edit. This card makes that impossible by
 * construction rather than by remembering: opening a stored profile fills every
 * line with REDACTED_HEADER, which the server reads as "keep what is stored",
 * and the only way to get an empty value into a line is to clear it by hand.
 * There is no code path that produces an empty value the user did not type.
 *
 * IT DOES NOT RIDE THE SETTINGS DRAFT. Profiles live behind their own routes
 * and never touch settings.json, precisely so a header value cannot land in the
 * diagnostics bundle. So this card fetches on mount and writes immediately,
 * like the account cards, and the shared Save bar knows nothing about it.
 *
 * THE ORIGIN IS THE MATCH AND THE ID IS THE NAME. Two different strings doing
 * two different jobs: a download is matched by its origin, and a Packagizer
 * rule addresses the profile by its id. An origin is scheme, host and port
 * together, so http and https are two sites, a different port is a third, and a
 * sub-domain is not covered by its parent. The hint says all of that, because
 * the alternative is somebody storing a working cookie under an origin nothing
 * ever matches and concluding the feature is broken.
 */

/** One line in the editor. `stored` marks a value that came from the server as
 *  a placeholder, so the form can tell "kept" apart from "typed". */
interface Line extends HeaderProfileLine {
  stored: boolean;
}

/** The row currently being edited, stored or brand new. */
interface Draft {
  /** Empty for a profile that does not exist yet. */
  original: string;
  id: string;
  origin: string;
  lines: Line[];
}

function draftFor(p: HeaderProfile): Draft {
  return {
    original: p.id,
    id: p.id,
    origin: p.origin,
    // REDACTED_HEADER and never '': this single line is the whole of the
    // promise in the doc comment above.
    lines: p.headers.map((name) => ({ name, value: REDACTED_HEADER, stored: true })),
  };
}

export function HeaderProfilesCard({ hue }: { hue: number }) {
  const { t } = useT();
  const [profiles, setProfiles] = useState<HeaderProfile[]>([]);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    let alive = true;
    void fetchHeaderProfiles().then(
      (list) => {
        if (alive) setProfiles(list);
      },
      () => {
        /* An empty table rather than a claim that nothing is stored: the Add
           button still works and a save answers with the real listing. */
      },
    );
    return () => {
      alive = false;
    };
  }, []);

  const run = async (work: () => Promise<HeaderProfile[] | void>) => {
    setBusy(true);
    setError('');
    try {
      const next = await work();
      if (next) setProfiles(next);
      else setProfiles(await fetchHeaderProfiles());
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
    setDraft({ original: '', id: '', origin: '', lines: [{ name: '', value: '', stored: false }] });
  };

  const save = () => {
    if (!draft) return;
    void run(() =>
      saveHeaderProfile(
        draft.origin.trim(),
        // The `stored` flag is dropped on the way out: the server only ever
        // sees a name and a value, and the placeholder IS the "keep it"
        // instruction. A line whose name was emptied is left out entirely
        // rather than sent as a nameless header.
        draft.lines.filter((l) => l.name.trim() !== '').map(({ name, value }) => ({ name: name.trim(), value })),
        draft.id.trim(),
      ),
    );
  };

  const patchLine = (i: number, next: Partial<Line>) =>
    setDraft((d) =>
      d ? { ...d, lines: d.lines.map((l, j) => (j === i ? { ...l, ...next } : l)) } : d,
    );

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle
        hint={t('settings.headerProfiles.hint')}
        right={
          <Button icon={<IconPlus width={16} height={16} />} disabled={busy || draft !== null} onClick={add}>
            {t('settings.headerProfiles.add')}
          </Button>
        }
      >
        {t('settings.headerProfiles.title')}
      </SectionTitle>

      {profiles.length === 0 && !draft ? (
        // Inside the card, not instead of it: Add is the way out of this state
        // and an EmptyState would take the button off the page with it.
        <p className="py-6 text-center text-sm text-carbon-textSub">
          {t('settings.headerProfiles.empty')}
          <span className="mt-1 block text-[11px] text-carbon-textMuted">
            {t('settings.headerProfiles.emptyHint')}
          </span>
        </p>
      ) : (
        <ul className="flex flex-col">
          {profiles.map((p, i) => (
            <li
              key={p.id}
              className={`group flex items-center gap-3 py-2.5 ${
                i === profiles.length - 1 && !draft ? '' : 'border-b border-carbon-border/60'
              }`}
            >
              <button
                type="button"
                className="flex min-w-0 flex-1 items-center gap-3 text-left"
                onClick={() => {
                  setError('');
                  setDraft(draftFor(p));
                }}
              >
                <span className="shrink-0 text-sm text-carbon-text">{p.id}</span>
                <span dir="ltr" title={p.origin} className="min-w-0 flex-1 truncate text-xs text-carbon-textSub">
                  {p.origin}
                </span>
                <span className="shrink-0 text-[11px] text-carbon-textMuted">
                  {t('settings.headerProfiles.count', { n: p.headers.length })}
                </span>
              </button>
              <IconBadge
                kind="danger"
                icon={<IconTrash width={14} height={14} />}
                hue={i}
                title={t('settings.headerProfiles.delete')}
                aria-label={`${t('settings.headerProfiles.delete')} · ${p.id}`}
                disabled={busy}
                className="opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100"
                onClick={() => {
                  // Confirmed, because it removes a signed-in session from this
                  // machine and there is nothing to undo it with: the value was
                  // never on this page to put back.
                  if (!window.confirm(t('settings.headerProfiles.deleteConfirm', { id: p.id, origin: p.origin })))
                    return;
                  void run(async () => {
                    await deleteHeaderProfile(p.id);
                  });
                }}
              />
            </li>
          ))}
        </ul>
      )}

      {draft && (
        <div className="glim-well flex flex-col gap-4 p-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t('settings.headerProfiles.name')} hint={t('settings.headerProfiles.nameHint')}>
              <TextInput
                dir="ltr"
                spellCheck={false}
                value={draft.id}
                placeholder={t('settings.headerProfiles.namePlaceholder')}
                // Read only once stored: the id is how a Packagizer rule
                // addresses this profile, so renaming it here would leave every
                // rule pointing at a profile that no longer answers, silently.
                readOnly={draft.original !== ''}
                className={draft.original !== '' ? 'cursor-default opacity-70' : ''}
                onChange={(e) => setDraft({ ...draft, id: e.target.value })}
              />
            </Field>
            <Field label={t('settings.headerProfiles.origin')} hint={t('settings.headerProfiles.originHint')}>
              <TextInput
                dir="ltr"
                spellCheck={false}
                value={draft.origin}
                placeholder="https://forum.example.org"
                onChange={(e) => setDraft({ ...draft, origin: e.target.value })}
              />
            </Field>
          </div>

          <div className="flex flex-col gap-2">
            {draft.lines.map((line, i) => (
              <div key={i} className="grid grid-cols-[minmax(0,12rem)_1fr_auto] items-end gap-2">
                <Field label={t('settings.headerProfiles.headerName')}>
                  <TextInput
                    dir="ltr"
                    spellCheck={false}
                    value={line.name}
                    onChange={(e) => patchLine(i, { name: e.target.value })}
                  />
                </Field>
                <Field
                  label={line.stored ? t('settings.headerProfiles.valueStored') : t('settings.headerProfiles.headerValue')}
                  hint={line.stored ? t('settings.headerProfiles.valueStoredHint') : undefined}
                >
                  <TextInput
                    dir="ltr"
                    spellCheck={false}
                    value={line.value}
                    // The moment somebody types, the line stops being "kept" and
                    // becomes a real value. Without this the placeholder would
                    // go back as a literal header value the first time anybody
                    // corrected a typo in it.
                    onChange={(e) => patchLine(i, { value: e.target.value, stored: false })}
                  />
                </Field>
                <IconBadge
                  kind="danger"
                  icon={<IconTrash width={14} height={14} />}
                  hue={i}
                  title={t('settings.headerProfiles.removeHeader')}
                  aria-label={t('settings.headerProfiles.removeHeader')}
                  onClick={() => setDraft({ ...draft, lines: draft.lines.filter((_, j) => j !== i) })}
                />
              </div>
            ))}
            <Button
              className="w-fit"
              icon={<IconPlus width={14} height={14} />}
              onClick={() => setDraft({ ...draft, lines: [...draft.lines, { name: '', value: '', stored: false }] })}
            >
              {t('settings.headerProfiles.addHeader')}
            </Button>
          </div>

          <div className="flex items-center gap-3">
            <Button
              disabled={busy || draft.id.trim() === '' || draft.origin.trim() === ''}
              onClick={save}
            >
              {t('settings.headerProfiles.save')}
            </Button>
            <Button kind="ghost" disabled={busy} onClick={() => setDraft(null)}>
              {t('common.cancel')}
            </Button>
            {error && <p className="text-xs text-statusWarn">{error}</p>}
          </div>
        </div>
      )}
    </Card>
  );
}

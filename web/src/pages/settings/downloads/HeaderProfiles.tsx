import { useEffect, useState } from 'react';
import { Button, Card, Field, IconBadge, Modal, SectionTitle, TextInput, useTooltip } from '../../../components/ui';
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

// Header profiles hold a person's own request headers for one site, such as a
// session cookie, a Referer or Basic auth. The values are sealed and never come
// back, so a stored profile opens with REDACTED_HEADER in every line, which the
// server reads as "keep", while an empty value clears the header. Profiles have
// their own routes, outside settings.json and the draft. A download matches a
// profile by origin (scheme, host and port); a Packagizer rule names it by id.

/** An editor line; `stored` marks a placeholder from the server, kept rather than typed. */
interface Line extends HeaderProfileLine {
  stored: boolean;
}

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
    // Never '', which would clear the stored header.
    lines: p.headers.map((name) => ({ name, value: REDACTED_HEADER, stored: true })),
  };
}

export function HeaderProfilesCard({ hue }: { hue: number }) {
  const { t } = useT();
  const [profiles, setProfiles] = useState<HeaderProfile[]>([]);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // The profile whose removal is being confirmed.
  const [confirming, setConfirming] = useState<HeaderProfile | null>(null);

  useEffect(() => {
    let alive = true;
    void fetchHeaderProfiles().then(
      (list) => {
        if (alive) setProfiles(list);
      },
      () => {
        /* Add still works, and a save answers with the real listing. */
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
      // The server's sentence names the field and what to send.
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
        // The placeholder itself tells the server to keep a value; a line
        // without a name is left out.
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
        // Inside the card rather than an EmptyState, which would hide Add.
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
                <OriginLine origin={p.origin} />
                <span className="shrink-0 text-[11px] text-carbon-textMuted">
                  {t('settings.headerProfiles.count', { n: p.headers.length })}
                </span>
              </button>
              <IconBadge
                // A lone glyph takes half its 32px badge.
                icon={<IconTrash width={16} height={16} />}
                hue={i}
                title={t('settings.headerProfiles.delete')}
                aria-label={`${t('settings.headerProfiles.delete')} · ${p.id}`}
                disabled={busy}
                className="opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100"
                onClick={() => {
                  // Confirmed, since the value cannot be put back.
                  setConfirming(p);
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
                // Read only once stored, since Packagizer rules refer to the id.
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
                    // Typing turns a kept line into a real value.
                    onChange={(e) => patchLine(i, { value: e.target.value, stored: false })}
                  />
                </Field>
                <IconBadge
                  icon={<IconTrash width={16} height={16} />}
                  hue={i}
                  title={t('settings.headerProfiles.removeHeader')}
                  aria-label={t('settings.headerProfiles.removeHeader')}
                  onClick={() => setDraft({ ...draft, lines: draft.lines.filter((_, j) => j !== i) })}
                />
              </div>
            ))}
            <Button
              className="w-fit"
              icon={<IconPlus width={16} height={16} />}
              onClick={() => setDraft({ ...draft, lines: [...draft.lines, { name: '', value: '', stored: false }] })}
            >
              {t('settings.headerProfiles.addHeader')}
            </Button>
          </div>

          {/* The spacer and the error come first so Save ends the row. The JSX
              order sets it, so the row mirrors in right-to-left languages. */}
          <div className="flex items-center gap-3">
            <span className="flex-1" />
            {error && <p className="text-xs text-statusWarn">{error}</p>}
            <Button kind="ghost" disabled={busy} onClick={() => setDraft(null)}>
              {t('common.cancel')}
            </Button>
            <Button
              disabled={busy || draft.id.trim() === '' || draft.origin.trim() === ''}
              onClick={save}
            >
              {t('settings.headerProfiles.save')}
            </Button>
          </div>
        </div>
      )}

      {confirming && (
        <Modal
          title={t('settings.headerProfiles.delete')}
          onClose={() => setConfirming(null)}
          footer={
            <>
              <span className="flex-1" />
              <Button kind="ghost" disabled={busy} onClick={() => setConfirming(null)}>
                {t('common.cancel')}
              </Button>
              <Button
                kind="secondary"
                disabled={busy}
                onClick={() => {
                  const id = confirming.id;
                  setConfirming(null);
                  void run(async () => {
                    await deleteHeaderProfile(id);
                  });
                }}
              >
                {t('settings.headerProfiles.delete')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-textSub">
            {t('settings.headerProfiles.deleteConfirm', { id: confirming.id, origin: confirming.origin })}
          </p>
        </Modal>
      )}
    </Card>
  );
}

/** OriginLine is its own component because the tooltip is a hook. */
function OriginLine({ origin }: { origin: string }) {
  const tip = useTooltip<HTMLSpanElement>(origin);
  // The span sits inside the row's edit button, so it takes no role and no
  // tab stop of its own.
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <span dir="ltr" {...tipHoverProps} className="min-w-0 flex-1 truncate text-xs text-carbon-textSub">
        {origin}
      </span>
      {tip.node}
    </>
  );
}

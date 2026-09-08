import { useEffect, useState } from 'react';
import { Button, Card, LabelBadge, SectionTitle } from '../../components/ui';
import { IconPlus } from '../../lib/icons';
import { useT } from '../../lib/i18n';
import {
  fetchEventTargets,
  fetchPlaceholders,
  type EventTargetRow,
  type EventTargetStatus,
  type Placeholder,
} from '../../lib/eventtargets';
import { FALLBACK_TRIGGERS, fetchScriptTriggers } from '../../lib/scripts';
import { useDraft, useFeatures } from './context';
import { TargetRow } from './eventtargets/TargetRow';

/**
 * Where this instance reports to when something happens: one row per address,
 * each with its own method, headers, body template and list of events.
 *
 * It is the outbound half of the event bus internal/script already publishes on.
 * The bus had exactly one subscriber (the script host) and no way for an
 * operator to add a second one without writing JavaScript, so "tell my phone
 * when a package finishes" was a scripting task. This page makes it four fields.
 *
 * SIX THINGS ABOUT THIS PAGE ARE DECISIONS RATHER THAN LAYOUT.
 *
 * IT IS NOT THE NOTIFICATIONS CARD, and the naming is deliberate the whole way
 * down. settings/look/Notifications.tsx routes an event to a toast or an OS
 * notification, never leaves the browser, is unavailable outright on an ordinary
 * plain-HTTP deployment, and already owns the word "Benachrichtigungen". This
 * one leaves the machine and reaches a server the operator named. Two controls
 * with one name and different reach is the confusion this page must not create,
 * so it is "Ereignisziele" / "Event targets" everywhere, including in the
 * package name on the other side.
 *
 * NOTHING IS FILLED IN AND NOTHING IS ON. A new row arrives switched off with no
 * event ticked, and both halves matter: a target that fired on all eleven events
 * the moment it was created would send two hundred messages the first time
 * somebody pasted a container, and an upgrade must not start sending because a
 * default said so. The server agrees by construction - `eventTargets` is absent
 * from every settings.json written before this existed and decodes to nil.
 *
 * THE AUTOSAVE TRAP is why three fields never reach the draft while they are
 * half typed. The settings shell saves 600 ms after any draft change, and
 * routes_settings.go's validateRows refuses the WHOLE settings PATCH when one
 * row fails, naming the row number. Typing "https" into an address, or "{" into
 * a body with a placeholder still being opened, would therefore fire a save, be
 * refused, and take every unrelated edit on every other settings page down with
 * it. The address, the headers and the body are held in the row's own state and
 * committed on the way out, exactly as Feeds.tsx already does for its address
 * and its title filter.
 *
 * THE HEALTH IS THE SERVER'S, NOT A GUESS, and it is blanked by a restart. An
 * absent lastAttempt means "nothing since the server started", never "never" -
 * the health table lives in memory beside the dispatcher and the targets
 * themselves live in settings.json, so a target that has been delivering for a
 * year reads as silent for the seconds after a container update. Drawing that as
 * a problem would be a false alarm on every boot.
 *
 * A HEADER VALUE IS A SECRET AND THIS BROWSER HAS NEVER SEEN ONE. The server
 * serves eight stars in place of every stored value and merges the real one back
 * on save, and only while the row still points at the same address. That last
 * clause is the security of the feature rather than a nicety: this browser is
 * what types the address, so a token that followed a changed one could be aimed
 * at a machine the client controls. It is why changing the address drops the
 * stored value, why the copy says so, and why the test button's answer shows
 * stars again where the token went.
 *
 * THE EVENTS ARE THE SCRIPT EDITOR'S EVENTS. The list comes from
 * GET /api/scripts/triggers, from the registry that actually fires them, and the
 * labels come from lib/triggers.ts, which both pages now read. A second list
 * here would offer events the server cannot honour and miss ones it can.
 */

/** A row that has been added but has no address the server would take, and
 *  therefore nothing that can safely be written into the shared draft yet. A
 *  blank row is DROPPED by notify.Sanitize, which is exactly what an untouched
 *  Add button produces, so putting it in the draft early would autosave it, get
 *  it deleted, and make it vanish under the cursor. */
interface PendingRow {
  key: string;
  row: EventTargetRow;
}

let pendingCounter = 0;
const freshKey = () => `p${(pendingCounter++).toString(36)}`;

/**
 * The lowest positive number no row is using, as a string.
 *
 * The SAME scheme notify.identify uses on the server, and assigned here rather
 * than left to the save for one concrete reason: the id is what the health table
 * joins on and what React keys the row by, so a row with no id until the save
 * round-trips is a row whose editor closes under whoever is typing in it.
 * identify keeps the first non-empty claim, so an id minted here survives.
 */
function nextId(rows: EventTargetRow[]): string {
  const taken = new Set(rows.map((r) => r.id));
  for (let n = 1; ; n++) {
    const id = String(n);
    if (!taken.has(id)) return id;
  }
}

/**
 * The health table, keyed by target id exactly as the server keys it.
 *
 * Fetched once on mount and NOT polled. The numbers in it change when an event
 * happens, which on a quiet instance is never, and a card that re-fetched every
 * few seconds would be asking a question whose answer cannot have changed,
 * forever, on a settings page left open in a background tab. A failure leaves
 * the map empty, so every row reads as "nothing since the server started" -
 * which is exactly what is true when nothing answered.
 */
function useHealth(): Record<string, EventTargetStatus> {
  const [health, setHealth] = useState<Record<string, EventTargetStatus>>({});
  useEffect(() => {
    let alive = true;
    void fetchEventTargets().then(
      (list) => {
        if (!alive) return;
        const byID: Record<string, EventTargetStatus> = {};
        for (const s of list) byID[s.id] = s;
        setHealth(byID);
      },
      () => {
        /* No status is drawn, which is the honest state when nothing answered. */
      },
    );
    return () => {
      alive = false;
    };
  }, []);
  return health;
}

/** The trigger vocabulary and the placeholder vocabulary, both from the server.
 *  Neither is ever guessed at here: a picker built from a list written on this
 *  side offers events the registry cannot fire and names the expander does not
 *  fill in, and both mistakes are invisible until somebody's message arrives
 *  wrong. */
function useVocabulary(): { triggers: string[]; placeholders: Placeholder[] } {
  const [triggers, setTriggers] = useState<string[]>(FALLBACK_TRIGGERS);
  const [placeholders, setPlaceholders] = useState<Placeholder[]>([]);
  useEffect(() => {
    let alive = true;
    void fetchScriptTriggers().then((list) => {
      if (alive) setTriggers(list);
    });
    void fetchPlaceholders().then((list) => {
      if (alive) setPlaceholders(list);
    });
    return () => {
      alive = false;
    };
  }, []);
  return { triggers, placeholders };
}

export function EventTargets() {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { features } = useFeatures();

  // Undefined and not null: the field is omitempty on the Go side, deliberately,
  // so that a settings.json written before this feature existed decodes to
  // nothing and this instance sends nothing.
  const rows = cfg.eventTargets ?? [];
  const health = useHealth();
  const { triggers, placeholders } = useVocabulary();

  // Switched off on the Modules page, which CLEARED settings.eventTargets and
  // parked the rows server-side, so anything typed here now would be typed into
  // a list the server is not reading. Tested on parked as well as enabled, never
  // on enabled alone: an empty list on a fresh install also reads as off, and
  // locking for that reason would leave nowhere to type the first address.
  const module = features.modules.find((m) => m.id === 'eventtargets');
  const parked = module !== undefined && !module.enabled && module.parked;

  const [openRow, setOpenRow] = useState('');
  const [pending, setPending] = useState<PendingRow[]>([]);

  // Never patch({ eventTargets: undefined }), which the settings shell's diff
  // would send as a changed key with no value. An emptied list goes out as []
  // and comes back absent, which is the same thing said the server's way.
  const write = (next: EventTargetRow[]) => patch({ eventTargets: next });

  const add = () => {
    const row: PendingRow = {
      key: freshKey(),
      // Off, with nothing ticked and no opinion about attempts or the time
      // limit. Every one of those zeroes is load-bearing - see this file's own
      // opening note.
      row: { id: '', name: '', enabled: false, url: '', attempts: 0, timeoutSeconds: 0 },
    };
    setPending((p) => [...p, row]);
    setOpenRow(row.key);
  };

  /** Move a pending row into the draft under the address that was typed, once
   *  the server would take it. */
  const commit = (key: string, next: EventTargetRow) => {
    const id = nextId(rows);
    write([...rows, { ...next, id }]);
    setPending((list) => list.filter((r) => r.key !== key));
    // The row is keyed by its id once it is stored, so it remounts here.
    // Without this the editor would close on whoever just finished typing.
    setOpenRow(id);
  };

  const total = rows.length + pending.length;

  return (
    <Card hue={0} className="flex flex-col gap-4">
      <SectionTitle
        hint={t('settings.eventTargets.titleHint')}
        right={
          <div className="flex items-center gap-2">
            {/* The module's state, not an explanation of it: the list below is
                dimmed, and this says which of the two reasons a list can be
                empty this one is. */}
            {parked && <LabelBadge label={t('settings.modules.off')} />}
            <Button icon={<IconPlus width={16} height={16} />} disabled={parked} onClick={add}>
              {t('settings.eventTargets.add')}
            </Button>
          </div>
        }
      >
        {t('settings.eventTargets.title')}
      </SectionTitle>

      {/* Dimmed and inert rather than hidden while the module is parked: a card
          that vanishes teaches nobody that the feature exists, and the switch
          that brings the stored targets back is one page away. */}
      <div className={parked ? 'pointer-events-none opacity-40' : ''}>
        {total === 0 ? (
          // Inside the card rather than instead of it: the Add button above is
          // the only way out of this state, and swapping the card for an
          // EmptyState would take it off the page.
          <p className="py-6 text-center text-sm text-carbon-textSub">
            {t('settings.eventTargets.empty')}
            <span className="mt-1 block text-[11px] text-carbon-textMuted">{t('settings.eventTargets.emptyHint')}</span>
          </p>
        ) : (
          <ul className="flex flex-col">
            {rows.map((row, i) => (
              <TargetRow
                key={row.id}
                row={row}
                index={i}
                last={i === total - 1}
                stored
                triggers={triggers}
                placeholders={placeholders}
                status={health[row.id]}
                open={openRow === row.id}
                onToggle={() => setOpenRow(openRow === row.id ? '' : row.id)}
                onChange={(next) => write(rows.map((r) => (r.id === row.id ? next : r)))}
                onRemove={() => write(rows.filter((r) => r.id !== row.id))}
              />
            ))}
            {pending.map((p, i) => (
              <TargetRow
                key={p.key}
                row={p.row}
                index={rows.length + i}
                last={rows.length + i === total - 1}
                stored={false}
                triggers={triggers}
                placeholders={placeholders}
                open={openRow === p.key}
                onToggle={() => setOpenRow(openRow === p.key ? '' : p.key)}
                // Kept in local state even when it cannot be stored yet, so
                // collapsing a row whose address is still half typed does not
                // throw away what was typed into it.
                onChange={(next) => setPending((list) => list.map((r) => (r.key === p.key ? { ...r, row: next } : r)))}
                onCommit={(next) => commit(p.key, next)}
                onRemove={() => setPending((list) => list.filter((r) => r.key !== p.key))}
              />
            ))}
          </ul>
        )}
      </div>
    </Card>
  );
}

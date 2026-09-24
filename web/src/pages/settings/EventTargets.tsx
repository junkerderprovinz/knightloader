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
 * EventTargetsCard lists the addresses this instance reports to when something
 * happens, each with its own method, headers, body template and events. It is
 * the outbound side of the event bus internal/script publishes on, and has a
 * name of its own so it is not taken for the in-browser notifications.
 *
 * A new row starts off with no event ticked. The address, headers and body are
 * committed on blur, because the server refuses the whole settings document
 * when one row is invalid. Header values come back as stars and are merged
 * back on save only while the row keeps its address, so a token cannot be
 * redirected to another machine.
 */

/**
 * PendingRow is a new row without a usable address. It stays out of the draft,
 * where notify.Sanitize would drop it on the next autosave.
 */
interface PendingRow {
  key: string;
  row: EventTargetRow;
}

let pendingCounter = 0;
const freshKey = () => `p${(pendingCounter++).toString(36)}`;

/**
 * nextId returns the lowest unused positive number, as notify.identify does.
 * The id is assigned here because the health table and the React key depend on
 * it before the save returns.
 */
function nextId(rows: EventTargetRow[]): string {
  const taken = new Set(rows.map((r) => r.id));
  for (let n = 1; ; n++) {
    const id = String(n);
    if (!taken.has(id)) return id;
  }
}

/**
 * useHealth fetches the health table once, keyed by target id. It changes only
 * when an event fires, so it is not polled.
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
        /* No status is drawn when nothing answered. */
      },
    );
    return () => {
      alive = false;
    };
  }, []);
  return health;
}

/** useVocabulary loads the trigger and placeholder lists from the server. */
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

export function EventTargetsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { features } = useFeatures();

  // omitempty, so an older settings.json sends nothing.
  const rows = cfg.eventTargets ?? [];
  const health = useHealth();
  const { triggers, placeholders } = useVocabulary();

  // The Modules page parks the rows and clears the list. Checked with `parked`,
  // because an empty list on a fresh install also reads as off.
  const module = features.modules.find((m) => m.id === 'eventtargets');
  const parked = module !== undefined && !module.enabled && module.parked;

  const [openRow, setOpenRow] = useState('');
  const [pending, setPending] = useState<PendingRow[]>([]);

  // An emptied list goes out as [], never undefined, which the shell's diff
  // would send as a key without a value.
  const write = (next: EventTargetRow[]) => patch({ eventTargets: next });

  const add = () => {
    const row: PendingRow = {
      key: freshKey(),
      // Off, nothing ticked, and no opinion on attempts or the time limit.
      row: { id: '', name: '', enabled: false, url: '', attempts: 0, timeoutSeconds: 0 },
    };
    setPending((p) => [...p, row]);
    setOpenRow(row.key);
  };

  /** Moves a pending row into the draft once its address is valid. */
  const commit = (key: string, next: EventTargetRow) => {
    const id = nextId(rows);
    write([...rows, { ...next, id }]);
    setPending((list) => list.filter((r) => r.key !== key));
    // The stored row is keyed by its id, so it remounts; keep it open.
    setOpenRow(id);
  };

  const total = rows.length + pending.length;

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle
        hint={t('settings.eventTargets.titleHint')}
        right={
          <div className="flex items-center gap-2">
            {/* While the module is off the badge replaces the Add button. */}
            {parked && <LabelBadge label={t('settings.modules.off')} />}
            {!parked && (
              <Button icon={<IconPlus width={16} height={16} />} onClick={add}>
                {t('settings.eventTargets.add')}
              </Button>
            )}
          </div>
        }
      >
        {t('settings.eventTargets.title')}
      </SectionTitle>

      {/* Parking clears the list on the server, so while the module is off
          only the empty-state sentence remains. */}
      <div>
        {total === 0 ? (
          // Inside the card rather than an EmptyState, which would hide Add.
          <p className="py-6 text-center text-sm text-carbon-textSub">
            {t('settings.eventTargets.empty')}
            {/* The invitation to add one goes with the Add button. */}
            {!parked && (
              <span className="mt-1 block text-[11px] text-carbon-textMuted">
                {t('settings.eventTargets.emptyHint')}
              </span>
            )}
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
                // Kept so collapsing a half-typed row keeps the text.
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

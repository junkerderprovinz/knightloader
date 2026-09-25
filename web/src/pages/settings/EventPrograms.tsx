import { useEffect, useState } from 'react';
import { Button, Card, SectionTitle } from '../../components/ui';
import { fetchDeploymentInfo } from '../../lib/api';
import {
  TIMEOUT,
  fetchEventPrograms,
  fetchProgramPlaceholders,
  newProgramId,
  type EventProgramRow,
  type EventProgramStatus,
} from '../../lib/eventprograms';
import type { Placeholder } from '../../lib/eventtargets';
import { IconPlus } from '../../lib/icons';
import { useT } from '../../lib/i18n';
import { FALLBACK_TRIGGERS, fetchScriptTriggers } from '../../lib/scripts';
import { useDraft } from './context';
import { ProgramRow } from './eventprograms/ProgramRow';
import { ModuleToggle } from './ModuleToggle';

/**
 * EventProgramsCard lists the programs this instance starts when something
 * happens, each with its arguments, its events and how many runs may go on at
 * once. It sits beside the event targets and uses their events and
 * placeholders, since both answer the same events.
 *
 * A new row is added with a name of its own, so the server keeps it on the
 * next save instead of dropping it as an untouched Add.
 */
export function EventProgramsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch, dirty } = useDraft();

  // omitempty, so an older settings.json sends nothing.
  const rows = cfg.eventPrograms ?? [];
  const { triggers, placeholders, deployment } = useVocabulary();
  const health = useHealth(dirty, rows);
  const [openRow, setOpenRow] = useState('');

  // An emptied list goes out as [], never undefined, which the shell's diff
  // would send as a key without a value.
  const write = (next: EventProgramRow[]) => patch({ eventPrograms: next });

  const add = () => {
    const id = newProgramId();
    write([
      ...rows,
      {
        id,
        name: t('settings.eventPrograms.newName', { n: rows.length + 1 }),
        enabled: false,
        command: { program: '', args: [], timeoutSeconds: TIMEOUT.fallback },
        parallel: 0,
      },
    ]);
    setOpenRow(id);
  };

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle
        hint={t('settings.eventPrograms.titleHint')}
        right={
          <Button icon={<IconPlus width={16} height={16} />} onClick={add}>
            {t('settings.eventPrograms.add')}
          </Button>
        }
      >
        {t('settings.module.eventprograms')}
      </SectionTitle>
      <ModuleToggle id="eventprograms" />

      <div>
        {rows.length === 0 ? (
          // Inside the card rather than an EmptyState, which would hide Add.
          <p className="py-6 text-center text-sm text-carbon-textSub">
            {t('settings.eventPrograms.empty')}
            <span className="mt-1 block text-[11px] text-carbon-textMuted">{t('settings.eventPrograms.emptyHint')}</span>
          </p>
        ) : (
          <ul className="flex flex-col">
            {rows.map((row, i) => (
              <ProgramRow
                key={row.id}
                row={row}
                index={i}
                last={i === rows.length - 1}
                triggers={triggers}
                placeholders={placeholders}
                status={health[row.id]}
                deployment={deployment}
                open={openRow === row.id}
                onToggle={() => setOpenRow(openRow === row.id ? '' : row.id)}
                onChange={(next) => write(rows.map((r) => (r.id === row.id ? next : r)))}
                onRemove={() => write(rows.filter((r) => r.id !== row.id))}
              />
            ))}
          </ul>
        )}
      </div>
    </Card>
  );
}

/** useVocabulary loads the events, the placeholders and the deployment once. */
function useVocabulary(): { triggers: string[]; placeholders: Placeholder[]; deployment: string } {
  const [triggers, setTriggers] = useState<string[]>(FALLBACK_TRIGGERS);
  const [placeholders, setPlaceholders] = useState<Placeholder[]>([]);
  const [deployment, setDeployment] = useState('');
  useEffect(() => {
    let alive = true;
    void fetchScriptTriggers().then((list) => {
      if (alive) setTriggers(list);
    });
    void fetchProgramPlaceholders().then((list) => {
      if (alive) setPlaceholders(list);
    });
    void fetchDeploymentInfo().then(
      (d) => {
        if (alive) setDeployment(d.deployment);
      },
      () => {
        /* the container's wording for a missing program stays out */
      },
    );
    return () => {
      alive = false;
    };
  }, []);
  return { triggers, placeholders, deployment };
}

/**
 * useHealth fetches the status table keyed by program id, again after every
 * save of a changed list, since whether a program resolves is part of it.
 */
function useHealth(dirty: boolean, rows: EventProgramRow[]): Record<string, EventProgramStatus> {
  const [health, setHealth] = useState<Record<string, EventProgramStatus>>({});
  const saved = dirty ? null : JSON.stringify(rows);
  useEffect(() => {
    if (saved === null) return;
    let alive = true;
    void fetchEventPrograms().then(
      (list) => {
        if (!alive) return;
        const byID: Record<string, EventProgramStatus> = {};
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
  }, [saved]);
  return health;
}

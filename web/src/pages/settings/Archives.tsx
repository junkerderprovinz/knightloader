import { useEffect, useState } from 'react';
import { Card, Field, FieldGroup, NumberInput, SectionTitle, TextArea, ToggleRow } from '../../components/ui';
import { PathInput } from '../../components/FolderPicker';
import { Tabs } from '../../components/Tabs';
import { fetchOptions, type ApiOptions } from '../../lib/api';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useDraft } from './context';

/**
 * Archives settles everything about an archive: whether it is unpacked, where
 * the files land, what happens to a name already taken and what becomes of the
 * archive afterwards.
 */

/**
 * useArchiveOptions reads the choices from GET /api/options. The extractor
 * accepts other collision policies than a download, so its lists come from the
 * server rather than from this file.
 */
function useArchiveOptions(): { options: ApiOptions | null; failed: boolean } {
  const [options, setOptions] = useState<ApiOptions | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let live = true;
    fetchOptions().then(
      (o) => live && setOptions(o),
      () => live && setFailed(true),
    );
    return () => {
      live = false;
    };
  }, []);

  return { options, failed };
}

// An id without a label shows as itself.
const COLLISION_LABEL: Partial<Record<string, TranslationKey>> = {
  overwrite: 'settings.archives.collision.overwrite',
  rename: 'settings.archives.collision.rename',
  skip: 'settings.archives.collision.skip',
};

const DISPOSAL_LABEL: Partial<Record<string, TranslationKey>> = {
  keep: 'settings.archives.disposal.keep',
  trash: 'settings.archives.disposal.trash',
  delete: 'settings.archives.disposal.delete',
};

export function Archives() {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { options, failed } = useArchiveOptions();

  const choices = (ids: string[] | undefined, labels: Partial<Record<string, TranslationKey>>) =>
    (ids ?? []).map((id) => ({ id, label: labels[id] ? t(labels[id]) : id }));

  // An older server may not send these fields.
  const extractTo = cfg.extractTo ?? '';
  const extractMoveTo = cfg.extractMoveTo ?? '';
  const disposal = cfg.archiveDisposal ?? 'keep';

  // What depends on a switch is absent until the switch is on. An empty
  // destination means "beside the archive", where a per-package subfolder
  // would only nest a folder.
  const unpacking = cfg.extract;
  const collecting = extractTo.trim() !== '';
  const keeping = disposal === 'keep';

  return (
    <div className="flex flex-col gap-10">
      <Card hue={0} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.archives.extractionTitle')}</SectionTitle>
        <ToggleRow hue={0} checked={cfg.extract} onChange={(v) => patch({ extract: v })} label={t('settings.extract')} />

        {/* Flush left under the switch, and absent while it is off. */}
        {unpacking && (
        <div className="flex flex-col gap-5">
          <Field
            layout="row"
            label={t('settings.archives.destination')}
            hint={`${t('settings.archives.destinationHint')} ${t('settings.pathVars')}`}
          >
            {/* The shared chooser browses the server, which knows what is mounted. */}
            <PathInput
              value={extractTo}
              placeholder={t('settings.archives.besideArchive')}
              title={t('settings.archives.destination')}
              onValue={(extractTo) => patch({ extractTo })}
            />
          </Field>

          {collecting && (
            <ToggleRow
              hue={1}
              checked={cfg.extractSubfolder ?? false}
              onChange={(v) => patch({ extractSubfolder: v })}
              label={t('settings.archives.subfolder')}
              hint={t('settings.archives.subfolderHint')}
            />
          )}

          {/* Where the finished files go once unpacked. A template counts as
              absolute by its fixed head (fixedPrefix), so a value with no fixed
              part is cleared on save, and the path is not probed until a move
              tries it. It applies with an empty destination too. */}
          <Field
            layout="row"
            label={t('settings.archives.moveTo')}
            hint={`${t('settings.archives.moveToHint')} ${t('settings.pathVars')}`}
          >
            <PathInput
              value={extractMoveTo}
              placeholder={t('settings.archives.besideArchive')}
              title={t('settings.archives.moveTo')}
              onValue={(extractMoveTo) => patch({ extractMoveTo })}
            />
          </Field>

          {/* FieldGroup, because a Field's label would pass a click on the
              caption to the first tab. */}
          {options && options.archiveCollisions.length > 0 && (
            <FieldGroup layout="row" label={t('settings.archives.collision')} hint={t('settings.archives.collisionHint')}>
              <Tabs
                label={t('settings.archives.collision')}
                variant="well"
                active={cfg.extractCollision ?? ''}
                onSelect={(extractCollision) => patch({ extractCollision })}
                items={choices(options.archiveCollisions, COLLISION_LABEL)}
              />
            </FieldGroup>
          )}
        </div>
        )}
      </Card>

      {/* The whole card goes while nothing is unpacked. */}
      {unpacking && (
      <Card hue={1} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.archives.afterwards')}</SectionTitle>
        <div className="flex flex-col gap-5">
          {options && options.archiveDisposals.length > 0 && (
            <FieldGroup
              layout="row"
              label={t('settings.archives.disposal')}
              // A container has no recycle bin: "trash" moves into a hidden
              // folder the server names and sweeps by age.
              hint={t('settings.archives.disposalHint', {
                folder: options.archiveTrashFolder,
              })}
            >
              <Tabs
                label={t('settings.archives.disposal')}
                variant="well"
                active={disposal}
                onSelect={(archiveDisposal) => patch({ archiveDisposal })}
                items={choices(options.archiveDisposals, DISPOSAL_LABEL)}
              />
            </FieldGroup>
          )}

          {disposal === 'trash' && (
            <Field
              label={t('settings.archives.retention')}
              hint={t('settings.archives.retentionHint', {
                folder: options?.archiveTrashFolder ?? '',
              })}
            >
              <NumberInput
                value={cfg.trashRetentionDays ?? 0}
                min={0}
                max={365}
                onValue={(v) => patch({ trashRetentionDays: v })}
              />
            </Field>
          )}

          {/* Absent while the archive is kept, since swept files go the same
              way as the archive. The sweep takes the package's own files, never
              the whole folder. */}
          {!keeping && (
            <ToggleRow
              checked={cfg.deleteInfoFiles ?? false}
              onChange={(v) => patch({ deleteInfoFiles: v })}
              label={t('settings.archives.infoFiles')}
              hint={t('settings.archives.infoFilesHint')}
            />
          )}
        </div>
      </Card>
      )}

      <Card hue={2} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.archivePasswords')}</SectionTitle>
        <Field label={t('settings.archivePasswords')} hint={t('settings.archivePasswordsHint')}>
          <TextArea
            rows={4}
            spellCheck={false}
            value={(cfg.archivePasswords ?? []).join('\n')}
            onChange={(e) =>
              patch({ archivePasswords: e.target.value.split('\n').filter((p) => p.trim() !== '') })
            }
          />
        </Field>
      </Card>

      {/* Only when the option lists did not arrive. */}
      {failed && <p className="text-xs text-statusFail">{t('settings.archives.optionsFailed')}</p>}
    </div>
  );
}

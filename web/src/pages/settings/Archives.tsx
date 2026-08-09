import { useEffect, useState } from 'react';
import { Card, Field, FieldGroup, NumberInput, SectionTitle, TextArea, Toggle } from '../../components/ui';
import { PathInput } from '../../components/FolderPicker';
import { Tabs } from '../../components/Tabs';
import { fetchOptions, type ApiOptions } from '../../lib/api';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useDraft } from './context';

/**
 * Everything about an archive: whether it is unpacked, where the files land,
 * what happens when something of that name is already there, and what becomes
 * of the archive itself afterwards.
 *
 * Three of those five had no home at all before this page: the extraction
 * destination, the collision policy and the disposal existed only in the
 * settings document, which meant the only way to reach them was the advanced
 * key table.
 */

/**
 * The choices this page offers come from GET /api/options, not from a list in
 * this file.
 *
 * Nor are they the download page's lists. An extraction honours a different set
 * of collision policies from a download - it has nobody to ask, and it decides
 * per folder rather than per file - so the server sends what the extractor
 * itself accepts. Hard-coding them here is how a build ends up offering a word
 * the server folds away on save, which looks exactly like the setting refusing
 * to stick.
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

// A server value is looked up rather than switched on, and an id with no string
// falls back to the id itself: a policy or a disposal a later build adds shows
// up in the strip under its own name instead of as a blank tab.
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

  // Read through a fallback, exactly as the password list is. The draft is
  // typed as a subset of a document the SERVER owns, so a field an older server
  // has not learnt to send yet arrives undefined - and the page has to render an
  // empty box for it rather than throw on a .trim() and take the whole settings
  // shell down with it.
  const extractTo = cfg.extractTo ?? '';
  const disposal = cfg.archiveDisposal ?? 'keep';

  // Off is not a reason to hide any of this - see the block below - but it is a
  // reason to grey it out, and the same is true one level down: an empty
  // destination means "beside the archive", where a per-package subfolder would
  // only nest a folder inside the folder that already names the package.
  const unpacking = cfg.extract;
  const collecting = extractTo.trim() !== '';
  const keeping = disposal === 'keep';

  return (
    <div className="flex flex-col gap-6">
      {/* What this build actually opens, in one line, taken from the extractor
          rather than written out here. It is drawn only once the server has
          answered: a page that names formats out of its own head goes on
          promising one the build stopped reading, and nothing anywhere catches
          it. A well rather than a card, because the page's first raised surface
          should be the settings themselves. */}
      {options && options.archiveFormats.length > 0 && (
        <div className="glim-well flex flex-wrap items-baseline gap-x-3 gap-y-1 px-3 py-2">
          <span className="glim-eyebrow shrink-0">{t('settings.archives.handles')}</span>
          <code dir="ltr" className="text-xs text-carbon-textSub">
            {options.archiveFormats.join('  ')}
          </code>
        </div>
      )}

      <Card className="flex flex-col gap-5">
        <Toggle checked={cfg.extract} onChange={(v) => patch({ extract: v })} label={t('settings.extract')} />

        {/* Indented under the switch they depend on, and disabled rather than
            hidden: a destination for extractions that never happen is a control
            that can only mislead, but removing it teaches nobody that the
            option exists. */}
        <div className={`flex flex-col gap-5 ps-6 transition-opacity ${unpacking ? '' : 'pointer-events-none opacity-40'}`}>
          <Field
            label={t('settings.archives.destination')}
            hint={`${t('settings.archives.destinationHint')} ${t('settings.pathVars')}`}
          >
            {/* The shared chooser and not a bare path box: it browses the
                SERVER, which is the only machine that knows what is mounted
                where, and picking a folder replaces only the fixed part of the
                value so a placeholder tail survives. See
                components/FolderPicker.tsx. */}
            <PathInput
              value={extractTo}
              placeholder={t('settings.archives.besideArchive')}
              title={t('settings.archives.destination')}
              onValue={(extractTo) => patch({ extractTo })}
            />
          </Field>

          {/* Wrapped so the switch can carry an (i): "does nothing without a
              destination" is not something a greyed-out control says for
              itself, and grey prose under it is what the bubble exists to
              replace. hideLabel because the caption above already says it. */}
          <div className={collecting ? '' : 'pointer-events-none opacity-40'}>
            <FieldGroup label={t('settings.archives.subfolder')} hint={t('settings.archives.subfolderHint')}>
              <Toggle
                checked={cfg.extractSubfolder ?? false}
                onChange={(v) => patch({ extractSubfolder: v })}
                label={t('settings.archives.subfolder')}
                hideLabel
              />
            </FieldGroup>
          </div>

          {/* FieldGroup and not Field: a Field is a `<label>`, and a label
              around a tab strip hands a click on the caption to the first tab -
              so clicking the word "If it is already there" would set the
              policy. See ui.tsx. */}
          {options && options.archiveCollisions.length > 0 && (
            <FieldGroup label={t('settings.archives.collision')} hint={t('settings.archives.collisionHint')}>
              <Tabs
                label={t('settings.archives.collision')}
                size="sm"
                className="w-fit"
                active={cfg.extractCollision ?? ''}
                onSelect={(extractCollision) => patch({ extractCollision })}
                items={choices(options.archiveCollisions, COLLISION_LABEL)}
              />
            </FieldGroup>
          )}
        </div>
      </Card>

      <SectionTitle>{t('settings.archives.afterwards')}</SectionTitle>
      <Card className="flex flex-col gap-5">
        <div className={`flex flex-col gap-5 ${unpacking ? '' : 'pointer-events-none opacity-40'}`}>
          {options && options.archiveDisposals.length > 0 && (
            <FieldGroup
              label={t('settings.archives.disposal')}
              // The bubble carries the whole truth about the middle answer,
              // because there is no recycle bin in a container to move anything
              // into: "trash" is a rename into a hidden folder plus a sweep by
              // age, and a setting that implies otherwise is a promise broken
              // quietly. The folder is named by the server so the two cannot
              // drift apart.
              hint={t('settings.archives.disposalHint', {
                folder: options.archiveTrashFolder,
              })}
            >
              <Tabs
                label={t('settings.archives.disposal')}
                size="sm"
                className="w-fit"
                active={disposal}
                onSelect={(archiveDisposal) => patch({ archiveDisposal })}
                items={choices(options.archiveDisposals, DISPOSAL_LABEL)}
              />
            </FieldGroup>
          )}

          {/* Only under "trash", where it is the difference between a folder
              that empties itself and one that grows forever. Hidden rather than
              disabled here: unlike the controls above it is not a capability
              anybody needs to be told about, it is a detail of the answer they
              have already chosen. */}
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

          {/* Disabled while the archive is being kept, because the sweep has no
              disposal of its own: a swept .nfo goes the same way the archive
              goes, so "keep everything" cannot coherently mean "keep the
              archive and destroy the notes beside it". */}
          <div className={keeping ? 'pointer-events-none opacity-40' : ''}>
            <FieldGroup
              label={t('settings.archives.infoFiles')}
              // The bubble says which files and, more to the point, how far the
              // sweep reaches: the package's own files and never the folder. On
              // the default layout one folder holds several releases, and a
              // sweep that read the folder would take the neighbours' notes.
              hint={t('settings.archives.infoFilesHint')}
            >
              <Toggle
                checked={cfg.deleteInfoFiles ?? false}
                onChange={(v) => patch({ deleteInfoFiles: v })}
                label={t('settings.archives.infoFiles')}
                hideLabel
              />
            </FieldGroup>
          </div>
        </div>
      </Card>

      <Card className="flex flex-col gap-5">
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

      {/* Said once, at the bottom, and only when the lists really did not
          arrive. The alternative is drawing the choosers from a list this build
          carries, which is the one thing the endpoint exists to prevent. */}
      {failed && <p className="text-xs text-statusFail">{t('settings.archives.optionsFailed')}</p>}
    </div>
  );
}

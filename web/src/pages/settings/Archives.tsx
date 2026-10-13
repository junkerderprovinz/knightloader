import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Card, Field, NumberInput, SectionTitle, ToggleRow } from '../../components/ui';
import { AfterDownload, type AfterDownloadSteps, type StationWays } from '../../components/pictures/AfterDownload';
import { Tabs } from '../../components/Tabs';
import { fetchOptions, type ApiOptions } from '../../lib/api';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useDraft } from './context';
import { requestJump, type JumpTarget } from './jump';
import { ModuleToggle } from './ModuleToggle';
import { ListArea, SettingPathInput, SettingRow } from './controls';
import { label as pageLabel } from './tx';

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

// The collision labels for every strip that offers a collision rule. An id
// without a label shows as itself.
export const COLLISION_LABEL: Partial<Record<string, TranslationKey>> = {
  overwrite: 'settings.archives.collision.overwrite',
  rename: 'settings.archives.collision.rename',
  skip: 'settings.archives.collision.skip',
};

export const DISPOSAL_LABEL: Partial<Record<string, TranslationKey>> = {
  keep: 'settings.archives.disposal.keep',
  trash: 'settings.archives.disposal.trash',
  delete: 'settings.archives.disposal.delete',
};

/** The row or card a station of the picture is set by. */
type StationSetting = Omit<JumpTarget, 'nonce'>;

const EXTRACTION: StationSetting = {
  page: 'archives',
  title: 'settings.module.extraction',
  label: 'settings.module.extraction',
};
const MOVE_UNPACKED: StationSetting = {
  page: 'archives',
  title: 'settings.module.extraction',
  label: 'settings.archives.moveTo',
};
const WORK_FOLDER: StationSetting = {
  page: 'downloads',
  title: 'settings.downloads.locationTitle',
  label: 'settings.downloads.workDir',
};

/**
 * stationSettings says where each step after a download is set. Moving has
 * two settings: the place for unpacked files on this page, which is offered
 * while archives are unpacked, and the working folder on Downloads. The
 * station leads to the one that is filled in, and to the one on this page
 * where both or neither are. Cleaning up is the archive's fate, whose row
 * exists only while archives are unpacked, so without that the switch comes
 * first.
 */
export function stationSettings(state: {
  unpacking: boolean;
  workDir: string;
  moveTo: string;
}): Record<keyof AfterDownloadSteps, StationSetting> {
  const { unpacking } = state;
  const here = unpacking && (state.moveTo.trim() !== '' || state.workDir.trim() === '');
  return {
    verify: { page: 'downloads', title: 'settings.downloads.limitsTitle', label: 'settings.module.checksums' },
    // The servers are a card of their own, with no single row that switches repairing on.
    repair: { page: 'accounts', title: 'accounts.usenet.title' },
    unpack: EXTRACTION,
    move: here ? MOVE_UNPACKED : WORK_FOLDER,
    cleanup: unpacking
      ? { page: 'archives', title: 'settings.archives.afterwards', label: 'settings.archives.disposal' }
      : EXTRACTION,
  };
}

export function Archives() {
  const { t } = useT();
  const navigate = useNavigate();
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

  // A file leaves its work folder for the download folder once it is checked
  // and unpacked, and unpacked files have a destination of their own.
  const moving = (cfg.workDir ?? '').trim() !== '' || (unpacking && extractMoveTo.trim() !== '');

  // Each station leads to its setting the way a result of the settings search
  // does: the row is asked for, then the page changes if it is another one.
  const settings = stationSettings({ unpacking, workDir: cfg.workDir ?? '', moveTo: extractMoveTo });
  const ways: StationWays = {};
  for (const step of Object.keys(settings) as (keyof AfterDownloadSteps)[]) {
    const to = settings[step];
    const setting = t(to.label ?? to.title);
    const elsewhere = to.page !== 'archives';
    ways[step] = {
      leadsTo: elsewhere
        ? t('picture.journey.wayTo', { setting, page: pageLabel(t, 'settings.nav.', to.page) })
        : t('picture.journey.wayHere', { setting }),
      go: () => {
        requestJump(to);
        if (elsewhere) navigate(`/settings/${to.page}`);
      },
    };
  }

  return (
    <div className="flex flex-col gap-10">
      <Card hue={0} className="flex flex-col gap-4">
        <SectionTitle
          hint={t('settings.archives.journeyHint', {
            verifying: t('status.verifying'),
            checksums: t('settings.module.checksums'),
            downloads: t('settings.nav.downloads'),
            repairing: t('status.repairing'),
            moving: t('picture.journey.move'),
            workDir: t('settings.downloads.workDir'),
            cleanup: t('picture.journey.cleanup'),
          })}
        >
          {t('settings.archives.journey')}
        </SectionTitle>
        <AfterDownload
          title={t('settings.archives.journey')}
          steps={{
            verify: cfg.verifyChecksums,
            // par2 only ever checks a release that came from a Usenet server of one's own.
            repair: (cfg.usenetServers ?? []).length > 0,
            unpack: unpacking,
            move: moving,
            cleanup: unpacking && !keeping,
          }}
          ways={ways}
        />
      </Card>

      <Card hue={1} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.module.extraction')}</SectionTitle>
        <ModuleToggle id="extraction" hue={1} />

        {/* Flush left under the switch, and absent while it is off. */}
        {unpacking && (
        <div className="flex flex-col gap-5">
          <SettingRow
            control="long"
            label={t('settings.archives.destination')}
            hint={`${t('settings.archives.destinationHint')} ${t('settings.pathVars')}`}
          >
            {/* The shared chooser browses the server, which knows what is mounted. */}
            <SettingPathInput
              field="extractTo"
              value={extractTo}
              placeholder={t('settings.archives.besideArchive')}
              title={t('settings.archives.destination')}
              onValue={(extractTo) => patch({ extractTo })}
            />
          </SettingRow>

          {collecting && (
            <ToggleRow
              hue={2}
              checked={cfg.extractSubfolder ?? false}
              onChange={(v) => patch({ extractSubfolder: v })}
              label={t('settings.archives.subfolder')}
              hint={t('settings.archives.subfolderHint')}
            />
          )}

          {/* Where the finished files go once unpacked. A template counts as
              absolute by its fixed head (fixedPrefix), so a value with no fixed
              part is refused on save, and the path is not probed until a move
              tries it. It applies with an empty destination too. */}
          <SettingRow
            control="long"
            label={t('settings.archives.moveTo')}
            hint={`${t('settings.archives.moveToHint')} ${t('settings.pathVars')}`}
          >
            <SettingPathInput
              field="extractMoveTo"
              value={extractMoveTo}
              placeholder={t('settings.archives.besideArchive')}
              title={t('settings.archives.moveTo')}
              onValue={(extractMoveTo) => patch({ extractMoveTo })}
            />
          </SettingRow>

          {options && options.archiveCollisions.length > 0 && (
            <SettingRow label={t('settings.archives.collision')} hint={t('settings.archives.collisionHint')}>
              <Tabs
                label={t('settings.archives.collision')}
                variant="well"
                size="sm"
                inline
                hug
                active={cfg.extractCollision ?? ''}
                onSelect={(extractCollision) => patch({ extractCollision })}
                items={choices(options.archiveCollisions, COLLISION_LABEL)}
              />
            </SettingRow>
          )}
        </div>
        )}

        {/* Outside the switch's rows: a category can unpack while the switch
            is off, and its archives need the passwords all the same. */}
        <Field label={t('settings.archivePasswords')} hint={t('settings.archivePasswordsHint')}>
          <ListArea rows={4} lines={cfg.archivePasswords} onLines={(archivePasswords) => patch({ archivePasswords })} />
        </Field>
      </Card>

      {/* The whole card goes while nothing is unpacked. */}
      {unpacking && (
      <Card hue={2} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.archives.afterwards')}</SectionTitle>
        <div className="flex flex-col gap-5">
          {options && options.archiveDisposals.length > 0 && (
            <SettingRow
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
                size="sm"
                inline
                hug
                active={disposal}
                onSelect={(archiveDisposal) => patch({ archiveDisposal })}
                items={choices(options.archiveDisposals, DISPOSAL_LABEL)}
              />
            </SettingRow>
          )}

          {disposal === 'trash' && (
            <SettingRow
              control="short"
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
            </SettingRow>
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

      {/* Only when the option lists did not arrive. */}
      {failed && <p className="text-xs text-statusFail">{t('settings.archives.optionsFailed')}</p>}
    </div>
  );
}

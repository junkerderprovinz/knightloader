import { Card, Field, Toggle, SectionTitle } from '../../components/ui';
import { PathInput } from '../../components/FolderPicker';
import { useT } from '../../lib/i18n';
import { useDraft } from './context';
import { useTx } from './tx';

// Where files land and what happens to a link the moment it arrives. The two
// belong together because they are the pair a new install has to answer before
// anything else works, and they are the only settings most people ever touch.
export function General() {
  const { t } = useT();
  const { tx } = useTx();
  const { cfg, patch } = useDraft();

  return (
    <div className="flex flex-col gap-6">
      <Card className="flex flex-col gap-5">
        <Field
          label={t('settings.downloadDir')}
          hint={`${t('settings.downloadDirHint')} ${t('settings.pathVars')}`}
        >
          {/* Still a box you can type a path into; the button beside it browses
              the server. Picking a folder replaces only the fixed part of the
              value - the <jd:…> tail is kept - which is the one thing a chooser
              on this field has to get right. See components/FolderPicker.tsx. */}
          <PathInput
            value={cfg.downloadDir}
            placeholder="/downloads"
            onValue={(downloadDir) => patch({ downloadDir })}
          />
        </Field>
        <Toggle
          checked={cfg.subfolderByPackage}
          onChange={(v) => patch({ subfolderByPackage: v })}
          label={t('settings.subfolderByPackage')}
        />
      </Card>

      <SectionTitle>{tx('settings.sectionIntake')}</SectionTitle>
      <Card className="flex flex-col gap-5">
        <Toggle checked={cfg.autoStart} onChange={(v) => patch({ autoStart: v })} label={t('settings.autoStart')} />
      </Card>
    </div>
  );
}

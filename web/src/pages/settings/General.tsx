import { Card, Field, TextInput, Toggle, SectionTitle } from '../../components/ui';
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
          {/* dir="ltr" on a path is not cosmetic: in an RTL locale a path with a
              trailing slash renders with the slash on the wrong end, which is a
              path the user cannot check by reading. */}
          <TextInput
            dir="ltr"
            value={cfg.downloadDir}
            placeholder="/downloads"
            spellCheck={false}
            onChange={(e) => patch({ downloadDir: e.target.value })}
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

import { Card, Field, TextArea, Toggle } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { useDraft } from './context';

export function Archives() {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  return (
    <Card className="flex flex-col gap-5">
      <div className="flex flex-col gap-3">
        <Toggle checked={cfg.extract} onChange={(v) => patch({ extract: v })} label={t('settings.extract')} />
        {/* Indented under the switch it depends on, and disabled rather than
            hidden: "delete the archive" with nothing unpacking archives is a
            control that can only mislead, but removing it teaches nobody that
            the option exists. */}
        <div className={`ps-6 transition-opacity ${cfg.extract ? '' : 'pointer-events-none opacity-40'}`}>
          <Toggle
            checked={cfg.deleteArchive}
            onChange={(v) => patch({ deleteArchive: v })}
            label={t('settings.deleteArchive')}
          />
        </div>
      </div>
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
  );
}

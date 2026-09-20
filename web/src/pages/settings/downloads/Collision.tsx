import { useEffect, useState } from 'react';
import { Card, Field, FieldGroup, NumberInput, SectionTitle } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { fetchOptions } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { useDraft } from '../context';

// The collision card: what a download does when its name is already taken in
// the destination folder, and how far "keep both" may count.

/**
 * The labels reuse the extraction page's keys, since both strips answer the
 * same question; the id lists stay apart because the server sends each its
 * own. An id without a label shows as itself.
 */
const COLLISION_LABEL: Partial<Record<string, TranslationKey>> = {
  rename: 'settings.archives.collision.rename',
  skip: 'settings.archives.collision.skip',
  overwrite: 'settings.archives.collision.overwrite',
};

export function CollisionCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  // The ids come from GET /api/options, which withholds collide.Ask because a
  // download has nobody to ask.
  const [policies, setPolicies] = useState<string[]>([]);
  useEffect(() => {
    let live = true;
    void fetchOptions().then(
      (o) => {
        if (live) setPolicies(o.collisionPolicies ?? []);
      },
      () => {
        /* the strip stays out rather than offering a guess at the policies */
      },
    );
    return () => {
      live = false;
    };
  }, []);

  const policyLabel = (id: string) => {
    const key = COLLISION_LABEL[id];
    return key ? t(key) : id;
  };

  // collide.ParsePolicy folds an empty value onto rename, the default.
  const policy = cfg.collisionPolicy || 'rename';
  const renaming = policy === 'rename';

  // The Go field is omitempty and absent whenever it is 0.
  const attempts = cfg.collisionMaxAttempts ?? 0;

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{t('settings.downloads.collisionTitle')}</SectionTitle>

      {/* FieldGroup, because a Field's label would pass a click on the
          caption to the first tab. */}
      {policies.length > 0 && (
        <FieldGroup
          layout="row"
          label={t('settings.downloads.collision')}
          hint={t('settings.downloads.collisionHint')}
        >
          <Tabs
            variant="well"
            label={t('settings.downloads.collision')}
            active={policy}
            onSelect={(collisionPolicy) => patch({ collisionPolicy })}
            items={policies.map((id) => ({ id, label: policyLabel(id) }))}
          />
        </FieldGroup>
      )}

      {/* Only "keep both" counts attempts, so the box is absent otherwise and
          comes back with its number unchanged. */}
      {renaming && (
        <Field label={t('settings.downloads.collisionAttempts')} hint={t('settings.downloads.collisionAttemptsHint')}>
          <NumberInput
            value={attempts}
            min={0}
            max={1000}
            step={1}
            // The bounds of sanitizeIntake. 0 means the package's own cap of
            // 1000, not "no limit".
            onValue={(v) => patch({ collisionMaxAttempts: Math.max(0, Math.min(1000, Math.round(v) || 0)) })}
          />
        </Field>
      )}
    </Card>
  );
}

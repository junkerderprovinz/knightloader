import { useEffect, useState } from 'react';
import { Card, Field, FieldGroup, NumberInput, SectionTitle } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { fetchOptions } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { useDraft } from '../context';

/**
 * What a download does when the name it wants is already taken in the
 * destination folder, and how far "keep both" is allowed to count.
 *
 * A card of its own rather than two more rows under the download folder: the
 * question is not where files land, it is what happens to a file that is
 * already lying there, and the second control here only means anything once
 * the first one has been answered. Both fields existed in the settings
 * document with no control anywhere, which is the kind of gap that reads as a
 * missing feature and is really a missing card.
 *
 * The card takes its palette position as a prop because the page decides the
 * order of its cards, and a badge sequence that jumps reads as a bug.
 */

/**
 * The three answers are looked up rather than switched on, and an id with no
 * string of its own falls back to the id: a policy a later build adds shows up
 * under its own name instead of as a blank tab, the same fallback the idle
 * action strip makes for the same reason.
 *
 * The KEYS are the extraction page's, on purpose. Both strips answer "something
 * of that name is already there", and two wordings for one question is how a
 * settings page teaches people that the same word means two different things.
 * The ID LISTS stay apart all the same: an extraction decides per folder and a
 * download per file, so the server sends each of them its own list
 * (internal/api/routes_settings.go) and neither may be built from the other's.
 */
const COLLISION_LABEL: Partial<Record<string, TranslationKey>> = {
  rename: 'settings.archives.collision.rename',
  skip: 'settings.archives.collision.skip',
  overwrite: 'settings.archives.collision.overwrite',
};

export function CollisionCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  // The ids come from GET /api/options and never from a list in this file.
  // collide.Ask is deliberately withheld by the server - there is nobody
  // sitting in front of it to answer, and no status to ask from - so a strip
  // written out here would offer a fourth answer that the save folds straight
  // back to "keep both".
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

  // An absent or empty value is not a fourth state and must not be drawn as
  // one: sanitizeIntake runs whatever is in the document through
  // collide.ParsePolicy (internal/settings/settings_intake.go), which folds
  // anything it does not recognise - the empty string included - onto rename,
  // which is also the shipped default. Reading it as anything else would grey
  // out the counter below on exactly the installs whose policy IS rename, and
  // leave the strip claiming nothing is chosen while the server has already
  // chosen.
  const policy = cfg.collisionPolicy || 'rename';
  const renaming = policy === 'rename';

  // Read through a fallback because the Go field is `omitempty` and is
  // genuinely absent from the JSON whenever it is 0, so this one is not
  // defensive but required.
  const attempts = cfg.collisionMaxAttempts ?? 0;

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{t('settings.downloads.collisionTitle')}</SectionTitle>

      {/* FieldGroup and not Field: a Field is a `<label>` and hands a click on
          its caption to the first control inside it, so a Field around a tab
          strip means clicking the words "If the file is already there" silently
          sets the policy to the first tab. See ui.tsx. */}
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

      {/* Dimmed and locked while the policy is not "keep both", because that is
          the only answer that counts anything - but not hidden: a number that
          disappears teaches nobody that the limit exists, and this is the one
          control on the card that can turn a runaway into a directory nobody
          can open. Opacity only, no pointer-events-none: the (i) is where the
          reason for the lock is written, and a wrapper that swallows pointer
          events would hide exactly the sentence that explains it. */}
      <div className={renaming ? '' : 'opacity-40'}>
        <Field label={t('settings.downloads.collisionAttempts')} hint={t('settings.downloads.collisionAttemptsHint')}>
          <NumberInput
            value={attempts}
            min={0}
            max={1000}
            step={1}
            disabled={!renaming}
            // The guard is repeated in the handler because NumberInput's two
            // arrows are siblings of the <input> and do not inherit its
            // disabled state: without this they would go on editing a field the
            // page has just said is dead. And the value is clamped to the same
            // bounds sanitizeIntake enforces (0 and collide.DefaultMaxAttempts,
            // internal/settings/settings_intake.go), so the box never shows a
            // number that the save is going to quietly turn into another one.
            // 0 stays a legal value here: it means the package's own cap of
            // 1000, not "no limit" and not "off".
            onValue={(v) => {
              if (!renaming) return;
              patch({ collisionMaxAttempts: Math.max(0, Math.min(1000, Math.round(v) || 0)) });
            }}
          />
        </Field>
      </div>
    </Card>
  );
}

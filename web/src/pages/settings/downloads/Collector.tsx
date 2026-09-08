import { useEffect, useState } from 'react';
import { Card, Field, FieldGroup, NumberInput, SectionTitle, ToggleRow } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { fetchOptions } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { useDraft } from '../context';

/**
 * What happens to a batch on its way OUT of the collector: how long it is left
 * lying there, what is done with a link that is already in the list, and where
 * the confirmed batch lands in the queue.
 *
 * All three had no control anywhere and were reachable only through the
 * advanced key table. They belong together because they are one moment - the
 * one where staged links become real downloads - and none of them says anything
 * useful next to the download folder.
 *
 * The card takes its palette position as a prop because the page decides the
 * order of its cards, and a badge sequence that jumps reads as a bug.
 */

/**
 * The confirm policies the server offers as an instance default: include,
 * exclude, exclude-and-remove and ask. Their labels are looked up here and an
 * id with no string of its own falls back to the id, which is the same
 * fallback the idle-action and collision strips make.
 *
 * Only the four ids THIS build knows get a translated label. An id a newer
 * server adds still renders as itself, which is worse than a word and far
 * better than a blank tab: it is a value somebody can recognise, search for
 * and report.
 *
 * Note the one key whose spelling does not match its id: the server says
 * 'exclude-and-remove' and the catalogue key is '.excludeAndRemove', because
 * the rest of the catalogue is camel case and one hyphenated key would be the
 * odd one out. That is exactly why this map exists instead of the key being
 * built by string concatenation from the id.
 */
const CONFIRM_LABEL: Partial<Record<string, TranslationKey>> = {
  include: 'settings.downloads.confirm.include',
  exclude: 'settings.downloads.confirm.exclude',
  'exclude-and-remove': 'settings.downloads.confirm.excludeAndRemove',
  ask: 'settings.downloads.confirm.ask',
};

export function CollectorCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  // The ids come from GET /api/options and never from a list in this file.
  // confirm.UseGlobal is withheld by the server, because a global default
  // cannot defer to itself, and a strip written out here would offer it.
  const [policies, setPolicies] = useState<string[]>([]);
  useEffect(() => {
    let live = true;
    void fetchOptions().then(
      (o) => {
        if (live) setPolicies(o.confirmPolicies ?? []);
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
    const key = CONFIRM_LABEL[id];
    return key ? t(key) : id;
  };

  // The countdown's master switch is `autoConfirm`, and it lives in the
  // Linkeingang card on the General page - one field, one control, so there is
  // deliberately no second toggle for it here. While it is off nothing ever
  // reads this number, so the field is dimmed and locked rather than offering a
  // countdown that cannot run. Its own hint already names the switch and the
  // page it is on, which is why the lock does not swallow pointer events: the
  // (i) is the only place that says where to go.
  const autoConfirming = cfg.autoConfirm;

  // Read through a fallback. The draft is typed as a subset of a document the
  // server owns, so a field an older server does not send arrives undefined and
  // would take the settings shell down on the first read.
  const delay = cfg.autoConfirmDelay ?? 0;

  // An absent or empty value is not a fifth state and must not be drawn as one:
  // sanitizeConfirm runs whatever is in the document through confirm.Parse
  // (internal/settings/settings_confirm.go), which folds everything it does not
  // recognise - the empty string and confirm.UseGlobal included - onto exclude,
  // which is also the shipped default. That fold exists so a corrupt file can
  // never turn the default into the one answer that deletes, and reading the
  // empty value as "nothing chosen" here would leave the strip claiming no
  // answer while the server has already given one.
  const dupes = cfg.onDupes || 'exclude';

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{t('settings.downloads.collectorTitle')}</SectionTitle>

      <div className={autoConfirming ? '' : 'opacity-40'}>
        <Field
          label={t('settings.downloads.autoConfirmDelay')}
          hint={t('settings.downloads.autoConfirmDelayHint')}
        >
          <NumberInput
            value={delay}
            min={0}
            max={86400}
            step={1}
            disabled={!autoConfirming}
            // The guard is repeated in the handler because NumberInput's two
            // arrows are siblings of the <input> and do not inherit its
            // disabled state: without this they would go on editing a field the
            // page has just said is dead. The ceiling is the server's own -
            // sanitizeConfirm cuts anything above a day back to a day and a
            // negative number to 0 (internal/settings/settings_confirm.go) - so
            // clamping here keeps the box from showing a number the save is
            // going to quietly turn into another one. 0 is not an off switch:
            // it confirms the instant a batch is staged, which is what every
            // install did before this field existed.
            onValue={(v) => {
              if (!autoConfirming) return;
              patch({ autoConfirmDelay: Math.max(0, Math.min(86400, Math.round(v) || 0)) });
            }}
          />
        </Field>
      </div>

      {/* FieldGroup and not Field: a Field is a `<label>` and hands a click on
          its caption to the first control inside it, so a Field around a tab
          strip means clicking the words "Links you already have" silently sets
          the policy to the first tab. See ui.tsx. */}
      {policies.length > 0 && (
        <FieldGroup layout="row" label={t('settings.downloads.onDupes')} hint={t('settings.downloads.onDupesHint')}>
          <Tabs
            variant="well"
            label={t('settings.downloads.onDupes')}
            active={dupes}
            onSelect={(onDupes) => patch({ onDupes })}
            items={policies.map((id) => ({ id, label: policyLabel(id) }))}
          />
        </FieldGroup>
      )}

      {/* ToggleRow and never a checkbox. No interlock of its own: it applies
          however the batch was confirmed - by hand, by the countdown above or
          out of the watch folder - because the queue applies it once, in
          app.startTasks, and every route into starting a collected batch goes
          through there. */}
      <ToggleRow
        checked={cfg.addAtTop ?? false}
        onChange={(addAtTop) => patch({ addAtTop })}
        label={t('settings.downloads.addAtTop')}
        hint={t('settings.downloads.addAtTopHint')}
      />
    </Card>
  );
}

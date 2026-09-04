import { useEffect, useState } from 'react';
import { Card, Field, FieldGroup, NumberInput, SectionTitle, TextInput, ToggleRow } from '../../components/ui';
import { PathInput } from '../../components/FolderPicker';
import { Tabs } from '../../components/Tabs';
import { fetchIdleActions, fetchOptions } from '../../lib/api';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useDraft, useFeatures } from './context';
import { useTx } from './tx';

// The end-of-queue action's menu labels - internal/idleaction.Actions() is the
// source of truth for WHICH ids exist (fetched below), this is only what each
// one reads as.
//
// These were hardcoded English until now, on the reasoning that the keys did
// not exist in en.ts. That reasoning kept a whole card in English on 41 of the
// 42 languages, which is a worse outcome than the work it saved: the keys are
// in every catalogue now. Only the two ids this build knows get a translated
// label; anything else (a newer backend's action) still renders as its raw id
// rather than as a blank tab, which is the same fallback IdleActionBanner
// makes for the same reason.
const IDLE_ACTION_KEYS: Record<string, TranslationKey> = {
  none: 'settings.downloads.idleActionNone',
  pause: 'settings.downloads.idleActionPause',
};

// Named DownloadsSettings and not Downloads: there is already a pages/Downloads
// page, and two components with one name in the same import graph is a mistake
// that compiles.
export function DownloadsSettings() {
  const { t } = useT();
  const { tx } = useTx();
  const { cfg, patch } = useDraft();
  const { features } = useFeatures();

  // The resume modes come from the server, like every other fixed choice on this
  // page's siblings. They were not offered at all until now: resumeOnStart was
  // read at boot and had no control anywhere, so the only way to set it was to
  // edit settings.json by hand - which is the kind of gap that looks like a
  // missing feature and is really a missing three lines.
  const [modes, setModes] = useState<string[]>([]);
  useEffect(() => {
    let live = true;
    void fetchOptions().then(
      (o) => {
        if (live) setModes(o.resumeModes ?? []);
      },
      () => {
        /* the strip stays out rather than offering a guess at the modes */
      },
    );
    return () => {
      live = false;
    };
  }, []);

  // An id this build has a key for reads in the user's language; anything else
  // reads as itself, which is still better than an empty tab.
  const idleActionLabel = (id: string) => {
    const key = IDLE_ACTION_KEYS[id];
    return key ? t(key) : id;
  };

  // Same shape, same reason, for the end-of-queue action's own menu - built
  // from the server's list (internal/idleaction.Actions) rather than
  // hardcoded here, so an id this build cannot carry out never appears as a
  // tab that does nothing when pressed.
  const [idleActions, setIdleActions] = useState<string[]>([]);
  useEffect(() => {
    let live = true;
    void fetchIdleActions().then(
      (a) => {
        if (live) setIdleActions(a);
      },
      () => {
        /* the row stays out rather than offering a guess at the actions */
      },
    );
    return () => {
      live = false;
    };
  }, []);

  // The registry, not the settings value, decides whether the folder field is
  // live. Switching the folder-watch module off clears the folder and parks it;
  // leaving the field editable here would let somebody type a folder back in and
  // restart the watcher while the modules page still reads "off". The two would
  // then disagree, which is exactly what a kill switch may not do.
  //
  // `parked` and not just `!enabled`: an empty folder on a fresh install is also
  // "off", and locking the field for that reason would leave nowhere to type the
  // first folder — the switch cannot turn on what was never set up either, so
  // the two would deadlock and folder watch would be unreachable forever.
  const watch = features.modules.find((m) => m.id === 'watch');
  const watchOff = watch !== undefined && !watch.enabled && watch.parked;

  return (
    <div className="flex flex-col gap-10">
      {/* Moved in from the now-removed Allgemein tab (jdp: "Wir brauchen
          keinen Allgemein Tab: das alles in den Download Tab verschieben") -
          where files land and what happens to a link the moment it arrives,
          the pair a new install has to answer before anything else works. */}
      <Card hue={0} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.downloads.locationTitle')}</SectionTitle>
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
        <ToggleRow
          checked={cfg.subfolderByPackage}
          onChange={(v) => patch({ subfolderByPackage: v })}
          label={t('settings.subfolderByPackage')}
        />
      </Card>

      <Card hue={1} className="flex flex-col gap-5">
        <SectionTitle>{tx('settings.sectionIntake')}</SectionTitle>
        {/* This toggle has always meant "skip the collector", which is
            autoConfirm's job since Wave 8 split the old single autoStart flag
            in three (settings.go's own doc comment). Binding it to the new,
            narrower autoStart field instead - an easy mistake once the old
            name and the new name coexist - would leave the one visible
            control on this page changing a field the label no longer
            describes, silently. */}
        <ToggleRow checked={cfg.autoConfirm} onChange={(v) => patch({ autoConfirm: v })} label={t('settings.autoStart')} />
      </Card>

      <Card hue={2} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.downloads.limitsTitle')}</SectionTitle>
        {/* The three counts that decide how much is open at once, together
            because they are read together: two downloads on one host, each
            pulled over eight sockets, is sixteen connections to that host and
            neither number says so on its own. */}
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Field label={t('settings.maxConcurrent')}>
            <NumberInput value={cfg.maxConcurrent} min={1} max={64} onValue={(v) => patch({ maxConcurrent: v })} />
          </Field>
          <Field label={t('settings.maxPerHost')}>
            <NumberInput value={cfg.maxPerHost} min={1} max={64} onValue={(v) => patch({ maxPerHost: v })} />
          </Field>
          {/* max is the engine's own bound, not a number picked here: a spinner
              that goes to 32 while every download opens 16 is a control that
              lies about what saving it did. */}
          <Field label={t('settings.chunks')} hint={t('settings.chunksHint')}>
            <NumberInput value={cfg.chunks} min={0} max={16} onValue={(v) => patch({ chunks: v })} />
          </Field>
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field label={t('settings.speedLimit')} hint={t('settings.speedHint')}>
            <NumberInput
              value={Math.round(cfg.speedLimit / 1024)}
              min={0}
              step={256}
              onValue={(v) => patch({ speedLimit: Math.max(0, v) * 1024 })}
            />
          </Field>
          <Field label={t('settings.maxRetries')} hint={t('settings.maxRetriesHint')}>
            <NumberInput value={cfg.maxRetries} min={0} max={20} onValue={(v) => patch({ maxRetries: v })} />
          </Field>
        </div>
        {modes.length > 0 && (
          <FieldGroup layout="row" label={t('settings.resumeOnStart')} hint={t('settings.resumeOnStartHint')}>
            <Tabs
              variant="well"
              label={t('settings.resumeOnStart')}
              active={cfg.resumeOnStart}
              onSelect={(id) => patch({ resumeOnStart: id })}
              items={modes.map((m) => ({ id: m, label: t(`settings.resume.${m}` as TranslationKey) }))}
            />
          </FieldGroup>
        )}

        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t('settings.keepFinishedDays')} hint={t('settings.keepFinishedDaysHint')}>
            <NumberInput
              value={cfg.keepFinishedDays}
              min={0}
              max={3650}
              onValue={(v) => patch({ keepFinishedDays: Math.max(0, v) })}
            />
          </Field>
          <Field label={t('settings.historyMax')} hint={t('settings.historyMaxHint')}>
            <NumberInput
              value={cfg.historyMax}
              min={0}
              max={1000000}
              onValue={(v) => patch({ historyMax: Math.max(0, v) })}
            />
          </Field>
        </div>

        <div className="flex flex-col gap-3">
          <ToggleRow hue={0} checked={cfg.crawl} onChange={(v) => patch({ crawl: v })} label={t('settings.crawl')} />
          <ToggleRow
            hue={1}
            checked={cfg.verifyChecksums}
            onChange={(v) => patch({ verifyChecksums: v })}
            label={t('settings.verifyChecksums')}
          />
          <ToggleRow
            hue={2}
            checked={cfg.preParserEnabled}
            onChange={(v) => patch({ preParserEnabled: v })}
            label={t('settings.preParser')}
            hint={t('settings.preParserHint')}
          />
        </div>
      </Card>

      <Card hue={3} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.downloads.watchTitle')}</SectionTitle>
        {/* Disabled rather than hidden: a field that vanishes teaches nobody
            that the folder watch exists, and the module page is where it is
            switched — which the info bubble says. */}
        <div className={watchOff ? 'pointer-events-none opacity-40' : ''}>
          <Field
            label={t('settings.watchDir')}
            hint={
              watchOff
                ? `${t('settings.watchDirHint')} ${tx('settings.downloads.watchOff')}`
                : t('settings.watchDirHint')
            }
          >
            <TextInput
              dir="ltr"
              value={cfg.watchDir}
              placeholder="/watch"
              spellCheck={false}
              disabled={watchOff}
              onChange={(e) => patch({ watchDir: e.target.value })}
            />
          </Field>
        </div>
      </Card>

      {idleActions.length > 0 && (
          <Card hue={4} className="flex flex-col gap-5">
          <SectionTitle>{t('settings.downloads.idleTitle')}</SectionTitle>
          <FieldGroup
            layout="row"
            label={t('settings.downloads.idleAction')}
            hint={t('settings.downloads.idleActionHint')}
          >
            <Tabs
              variant="well"
              label={t('settings.downloads.idleAction')}
              active={cfg.idleAction.action}
              onSelect={(id) => patch({ idleAction: { ...cfg.idleAction, action: id } })}
              items={idleActions.map((id) => ({ id, label: idleActionLabel(id) }))}
            />
          </FieldGroup>

          {cfg.idleAction.action !== 'none' && (
            <Field
              label={t('settings.downloads.idleCountdown')}
              hint={t('settings.downloads.idleCountdownHint')}
            >
              <NumberInput
                value={cfg.idleAction.delaySeconds}
                min={5}
                max={86400}
                onValue={(v) => patch({ idleAction: { ...cfg.idleAction, delaySeconds: v } })}
              />
            </Field>
          )}
          </Card>
      )}
    </div>
  );
}

import { useEffect, useState } from 'react';
// TextInput went with the watch folder when that left this page and the import
// stayed behind, which nothing catches: noUnusedLocals is off in web/tsconfig.
// It is in use again since the working folder landed in the Speicherort card.
import { Card, Field, FieldGroup, NumberInput, SectionTitle, TextArea, TextInput, ToggleRow } from '../../components/ui';
import { PathInput } from '../../components/FolderPicker';
import { Tabs } from '../../components/Tabs';
import { fetchOptions } from '../../lib/api';
import { useT, type TranslationKey } from '../../lib/i18n';
import { isLeet } from '../../lib/leet';
import { useDraft } from './context';
// Eight cards that each own one subject, in their own files under ./downloads.
// They live beside this page rather than inside it because this file was the
// natural home for all eight and would have been the wrong one: a page that
// answers twelve questions in one 1500-line component is a file nobody can edit
// two things in at once. Seven of them read and write the same shared draft
// through useDraft, so splitting them costs nothing at runtime, and each takes
// its hue as a prop because the ORDER of the cards is this page's decision and
// a jumbled badge sequence reads as a bug.
//
// HeaderProfiles is the exception and the reason is worth knowing: its values
// live in the encrypted credential store behind their own routes, never in
// settings.json, so that a header value cannot reach the diagnostics bundle.
// That card therefore saves itself and the bar at the bottom knows nothing
// about it.
import { CollisionCard } from './downloads/Collision';
import { CollectorCard } from './downloads/Collector';
import { DiskSpaceCard } from './downloads/DiskSpace';
import { FeedsCard } from './downloads/Feeds';
import { HeaderProfilesCard } from './downloads/HeaderProfiles';
import { FolderCheckCard } from './downloads/FolderCheck';
import { IdleActionCard } from './downloads/IdleAction';
import { HostRulesCard } from './downloads/HostRules';
import { MediaHooksCard } from './downloads/MediaHooks';
import { StallCard } from './downloads/Stall';
import { VolumeCapCard } from './downloads/VolumeCap';

// Named DownloadsSettings and not Downloads: there is already a pages/Downloads
// page, and two components with one name in the same import graph is a mistake
// that compiles.
export function DownloadsSettings() {
  const { t } = useT();
  const { cfg, patch } = useDraft();

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
        {/* Beside the download folder rather than on a page of its own,
            because the two are one question asked twice: where the bytes are
            written while they arrive, and where the finished file ends up.
            Reading them apart is how somebody sets a working folder on the
            same disk they were trying to keep clear.

            A plain TextInput and NOT the PathInput above it. PathInput exists
            to keep a <jd:…> tail when you browse, and this field may not have
            one: several downloads heading for one destination share this
            single folder, so a template would scatter the parts of a
            multi-volume archive across four of them and no set would ever
            unpack. sanitizeStaging drops a template here, and a chooser that
            offered to build one would be offering to have it thrown away. */}
        <Field label={t('settings.downloads.workDir')} hint={t('settings.downloads.workDirHint')}>
          <TextInput
            value={cfg.workDir}
            placeholder="/downloads/.incoming"
            spellCheck={false}
            dir="ltr"
            onChange={(e) => patch({ workDir: e.target.value })}
          />
        </Field>
      </Card>

      {/* Directly under the folders, because it is the question those folders
          raise: what happens when the name is already taken there. */}
      <CollisionCard hue={1} />

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
            <span className="flex items-center gap-2">
              <span className="min-w-0 flex-1">
                <NumberInput
                  value={Math.round(cfg.speedLimit / 1024)}
                  min={0}
                  step={256}
                  onValue={(v) => patch({ speedLimit: Math.max(0, v) * 1024 })}
                />
              </span>
              {/* 1337 (docs/easter-eggs.md). THE NUMBER STAYS IN THE FIELD and the
                word stands beside it. A field that shows something other than
                what was typed into it is a bug for one second before it is a
                joke, and the second is the one that gets reported.

                THE UNIT IS THE HALF THAT IS EASY TO GET WRONG. settings.speedLimit
                is BYTES per second on the wire (lib/api.ts) and this field draws
                and writes KiB, which is what the /1024 above and the *1024 beside
                it are. So the comparison is against 1337 KiB in bytes and never
                against the raw setting, which would be 1337 B/s - a limit nobody
                would ever type and an egg nobody would ever find. isLeet is
                derived from cfg, not from the keystroke, so it follows a value
                pasted in, stepped to, or restored from a settings import just as
                it follows one typed.

                  BESIDE THE NUMBER AND NOT UNDER IT. Dropped straight into the
                  Field it stacked below the input, which puts it exactly where
                  this page's hints and error lines live - so the joke read as a
                  validation message about the number above it. A word to the
                  right of a field is an annotation; a word underneath one is a
                  complaint.

                  THE FIELD GETS NARROWER WHILE THE WORD STANDS, and it is left
                  that way on purpose. Measured on the live page: 420px with any
                  other limit, 383.9px with 1337 - the word takes its 36px out
                  of the `flex-1` beside it. Both alternatives are worse. Hold
                  the space open always and a secret shapes the layout when it
                  is OFF, which is precisely what docs/easter-eggs.md's one rule
                  forbids, and it would hold a different width open in each of
                  42 languages. Lift the word out of the flow and it overlaps
                  the number as soon as the panel is narrow. What is left is a
                  field that still has room for far more digits than the five a
                  KiB/s limit can hold, so nothing is cut off and the number
                  goes on reading exactly as typed.

                  Off is typing a different number, which is also how it is
                  switched on: there is no state anywhere and nothing to reset. */}
              {isLeet(cfg.speedLimit) && (
                <span className="shrink-0 text-[11px] leading-none text-carbon-textMuted">
                  {t('settings.motion.storm')}
                </span>
              )}
            </span>
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
          {/* "Seiten crawlen" itself left this card for the crawl card below
              (jdp, 2026-09-07): the switch and the four numbers that shape what
              it does are one idea, and a master toggle sitting three controls
              away from its own settings is the arrangement where somebody
              raises the depth on an install that has crawling switched off. */}
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

      {/* Immediately after the three counts it makes exceptions to. The card
          above says what every hoster gets; this one says which hoster is
          different, and reading them apart is how somebody throttles the whole
          instance down to its most delicate hoster. */}
      <HostRulesCard hue={3} />

      {/* Beside the per-hoster numbers because it answers the same shape of
          question, what THIS site needs that the others do not, and because a
          site that wants its own headers usually wants its own connection
          count too. Its values live in the credential store and not in the
          settings document, so this card saves on its own rather than through
          the bar at the bottom. */}
      <HeaderProfilesCard hue={4} />

      {/* Then the two guards that stop a download rather than shape it: one
          watches the clock, one watches the disk. */}
      <StallCard hue={5} />
      <DiskSpaceCard hue={6} />

      {/* Beside the disk floors and for the reason they are a card of their own:
          both are guards that decide whether a download STARTS, measured against
          something outside the queue. The disk answers "is there room for this
          file", the cap answers "is there allowance left this period". The two
          are in DIFFERENT units on purpose, binary GiB up there and decimal GB
          down here, and each card says so. */}
      <VolumeCapCard hue={7} />

      {/* What happens to links on the way IN, before any of the above applies. */}
      <CollectorCard hue={8} />

      {/* The crawl, on its own, because it is the one thing on this page that
          sends requests to a server nobody here runs.

          EVERYTHING BELOW THE MASTER SWITCH IS ABSENT WHILE IT IS OFF, NOT
          DIMMED (GlimStone 1.10.0, and the test 1.16.0 settled it with: does
          the control still do anything?). No crawl runs while the switch is
          off, so a depth, a page cap, a same-host rule and two pattern lists
          read by nobody are six controls somebody can see, read and reach for
          that answer nothing - with the reason one row up, which is exactly
          where nobody looks once they have decided this row is the interesting
          one. The switch itself stays, because that is the control somebody IS
          looking for.

          The dimmed-and-inert wrapper this replaces also composited its own
          explanation away: opacity applies to a whole subtree, so the (i)
          bubbles inside it rendered at 40% too (1.9.0). */}
      <Card hue={9} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.crawl.title')}</SectionTitle>
        <ToggleRow hue={0} checked={cfg.crawl} onChange={(v) => patch({ crawl: v })} label={t('settings.crawl')} />

        {cfg.crawl && (
        <div className="flex flex-col gap-5">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            {/* max is the crawler's own ceiling (internal/crawler.MaxDepth), not
                a number picked here: a spinner that goes to 10 while the server
                clamps at 3 is a control that lies about what saving it did. */}
            <Field label={t('settings.crawl.depth')} hint={t('settings.crawl.depthHint')}>
              <NumberInput value={cfg.crawlDepth} min={1} max={3} onValue={(v) => patch({ crawlDepth: v })} />
            </Field>
            {/* A depth of 1 is one page, so a page CAP counts nothing and a
                same-host rule has no second host to compare against: both hang
                off the number beside them rather than off the switch above, and
                both go for the same reason. */}
            {cfg.crawlDepth >= 2 && (
            <Field label={t('settings.crawl.maxPages')} hint={t('settings.crawl.maxPagesHint')}>
              <NumberInput value={cfg.crawlMaxPages} min={1} max={200} onValue={(v) => patch({ crawlMaxPages: v })} />
            </Field>
            )}
          </div>

          {cfg.crawlDepth >= 2 && (
          <ToggleRow
            hue={1}
            checked={cfg.crawlSameHost}
            onChange={(v) => patch({ crawlSameHost: v })}
            label={t('settings.crawl.sameHost')}
            hint={t('settings.crawl.sameHostHint')}
          />
          )}

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label={t('settings.crawl.include')} hint={t('settings.crawl.includeHint')}>
              <TextArea
                rows={3}
                spellCheck={false}
                value={(cfg.crawlInclude ?? []).join('\n')}
                onChange={(e) => patch({ crawlInclude: e.target.value.split('\n').filter((p) => p.trim() !== '') })}
              />
            </Field>
            <Field label={t('settings.crawl.exclude')} hint={t('settings.crawl.excludeHint')}>
              <TextArea
                rows={3}
                spellCheck={false}
                value={(cfg.crawlExclude ?? []).join('\n')}
                onChange={(e) => patch({ crawlExclude: e.target.value.split('\n').filter((p) => p.trim() !== '') })}
              />
            </Field>
          </div>
        </div>
        )}
      </Card>

      {/* "Sammler überspringen" and the watch folder both left this page for
          the Linkeingang card on the General tab (jdp, 2026-09-07, after four
          independent proposals for restructuring the settings were weighed and
          only this one survived). Both are ways a link gets IN, which is what
          that card is about, and both were the only thing in a card of their
          own here. Nothing else moved: the analysis found that every further
          merge cost more in search words and bookmarks than it bought. */}
      {/* The other way links arrive without anybody pasting them, and the
          server itself points people here for it: setFeature's refusal says
          "add a feed on the Downloads page". */}
      <FeedsCard hue={10} />

      {/* Last, because it is the only card here about what happens once
          everything above has finished. It moved into a file of its own the
          moment it grew a command to run, a preflight for that command and a
          report on the last one - see ./downloads/IdleAction.tsx, which owns
          its own fetches (the action menu and the deployment both come from
          the server, so the menu can never offer an action this build cannot
          carry out) and renders nothing at all until they answer. */}
      <IdleActionCard hue={11} />

      {/* APPENDED AT THE END ON PURPOSE, and it does not belong here yet.
          Its subject is the folder fields near the top of this page - it
          measures what a file written into each of them actually comes out
          as - so its place is beside the Speicherort card. Inserting it there
          renumbers eight hues in the one file several waves are editing at
          once, and a badge sequence that jumps reads as a bug in every one of
          those diffs. So: appended now with the next free hue, moved up later
          in a commit that touches nothing else. Do not do both halves at
          once. */}
      <FolderCheckCard hue={12} />

      {/* Appended for the same reason as the card above it, and its own place
          is a different one again: what happens once a package is FINISHED
          belongs beside the end-of-queue action, not below a folder check.
          Inserting it there renumbers the hues of every card after it in the
          one file several waves are editing at once, and a badge sequence that
          jumps reads as a bug in each of those diffs. Next free hue now, moved
          into place later in a commit that touches nothing else. */}
      <MediaHooksCard hue={13} />
    </div>
  );
}

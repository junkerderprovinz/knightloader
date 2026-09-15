import { type ReactNode, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useT } from '../../lib/i18n';
import { Card, SectionTitle } from '../../components/ui';
import { fetchHealth } from '../../lib/api';
import { IconCrest, IconGithub, IconMail } from '../../lib/icons';
import { IconBitcoin, IconBuyMeACoffee, IconPayPal } from '../../components/donateMarks';
import { CryptoDonateDialog } from '../../components/CryptoDonateDialog';

/**
 * The help page: what this build actually does, organised by the question a
 * person has ("how do I get past a hoster limit", "where do encrypted zips
 * get their password"), each pointing at the settings page that controls it.
 *
 * Content-driven in the sense that build-plan.md's 10C brief means it -
 * grounded in what this build actually ships (internal/api/routes_features.go's
 * own registry, and the packages it names, is the ground truth this was
 * checked against) rather than a static copy of marketing prose that drifts
 * the day a feature changes shape. It is not, however, generated from that
 * registry at runtime: a registry row's Reason is one English sentence aimed
 * at "why is this switch grey", not the paragraph a person opening a help
 * page actually wants, so the two stay separate documents that happen to
 * agree today rather than one page pretending to be both.
 *
 * Every settings.help.* key this page reads lives in en.ts (and is typed
 * TranslationKey, checked against every other locale) - read straight through
 * useT() below, same as any other page. The count is deliberately not written
 * out here: it said 47 while the file read 46, which is what a hand-kept
 * number does the first time a Topic gains or loses a bullet.
 * check-docs-claims.mjs checks the relationship that actually matters instead,
 * that the two sets are equal in both directions, so a key read here but absent
 * from en.ts and a key sitting in en.ts that nothing renders both fail loudly.
 */
function Topic({
  title,
  children,
  links,
  hue,
}: {
  title: string;
  children: ReactNode;
  links?: { to: string; label: string }[];
  hue?: number;
}) {
  return (
      <Card hue={hue} className="flex flex-col gap-3">
        <SectionTitle>{title}</SectionTitle>
        <div className="flex flex-col gap-2 text-sm text-carbon-textSub">{children}</div>
        {links && links.length > 0 && (
          <div className="flex flex-wrap gap-x-4 gap-y-1 pt-1">
            {links.map((l) => (
              <Link
                key={l.to}
                to={l.to}
                className="text-xs text-carbon-textMuted underline-offset-2 hover:text-carbon-text hover:underline"
              >
                {l.label} →
              </Link>
            ))}
          </div>
        )}
      </Card>
  );
}

function Bullets({ items }: { items: string[] }) {
  return (
    <ul className="flex flex-col gap-1.5 pl-4">
      {items.map((it, i) => (
        <li key={i} className="list-disc marker:text-carbon-textMuted">
          {it}
        </li>
      ))}
    </ul>
  );
}

export function Help() {
  const { t } = useT();

  return (
    <div className="flex flex-col gap-10">
      <Topic
        title={t('settings.help.intake.title')}
        hue={0}
        links={[
          { to: '/settings/downloads', label: t('settings.help.intake.link1') },
          { to: '/settings/access', label: t('settings.help.intake.link2') },
        ]}
      >
        <p>{t('settings.help.intake.body')}</p>
        <Bullets
          items={[
            t('settings.help.intake.b1'),
            t('settings.help.intake.b2'),
            t('settings.help.intake.b3'),
            t('settings.help.intake.b4'),
          ]}
        />
      </Topic>

      <Topic
        title={t('settings.help.collector.title')}
        hue={1}
        // '/settings/general' was not a page id, it was a guess at one, and
        // Settings.tsx routes an unknown id silently to Downloads - so this
        // link had always landed on the wrong page with no error anywhere.
        // The General tab's id is and stays 'look' (renaming it would break
        // every bookmark and the stored settingsTabOrder).
        links={[{ to: '/settings/look', label: t('settings.help.collector.link') }]}
      >
        <p>{t('settings.help.collector.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.rules.title')}
        hue={2}
        links={[{ to: '/settings/rules', label: t('settings.help.rules.link') }]}
      >
        <p>{t('settings.help.rules.body')}</p>
      </Topic>

      <Topic title={t('settings.help.queue.title')} hue={3}>
        <p>{t('settings.help.queue.body')}</p>
        <Bullets items={[t('settings.help.queue.b1'), t('settings.help.queue.b2')]} />
      </Topic>

      <Topic
        title={t('settings.help.limits.title')}
        hue={4}
        links={[
          { to: '/settings/connections', label: t('settings.help.limits.link1') },
          { to: '/settings/reconnect', label: t('settings.help.limits.link2') },
          // /accounts, not /settings/accounts: registry.tsx's PAGES map still
          // leaves "accounts" deliberately absent (its own comment there), so
          // that tab renders the empty-state placeholder. The real, working
          // page has always been the main nav's own /accounts (router.tsx).
          { to: '/accounts', label: t('settings.help.limits.link3') },
        ]}
      >
        <p>{t('settings.help.limits.body')}</p>
        <Bullets items={[t('settings.help.limits.b1'), t('settings.help.limits.b2'), t('settings.help.limits.b3')]} />
      </Topic>

      <Topic
        title={t('settings.help.captcha.title')}
        hue={5}
        links={[{ to: '/settings/captcha', label: t('settings.help.captcha.link') }]}
      >
        <p>{t('settings.help.captcha.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.after.title')}
        hue={6}
        links={[
          { to: '/settings/archives', label: t('settings.help.after.link1') },
          { to: '/settings/downloads', label: t('settings.help.after.link2') },
        ]}
      >
        <p>{t('settings.help.after.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.schedule.title')}
        hue={7}
        links={[{ to: '/settings/schedule', label: t('settings.help.schedule.link') }]}
      >
        <p>{t('settings.help.schedule.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.instances.title')}
        hue={8}
        links={[{ to: '/instances', label: t('settings.help.instances.link') }]}
      >
        <p>{t('settings.help.instances.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.access.title')}
        hue={9}
        links={[
          { to: '/settings/access', label: t('settings.help.access.link1') },
          { to: '/settings/diagnostics', label: t('settings.help.access.link2') },
        ]}
      >
        <p>{t('settings.help.access.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.advanced.title')}
        hue={10}
        links={[{ to: '/settings/advanced', label: t('settings.help.advanced.link') }]}
      >
        <p>{t('settings.help.advanced.body')}</p>
      </Topic>

      {/* The Über card is not here any more (jdp, 2026-09-07: "die Über-card
          soll in den allgemein-tab ganz nach unten"). It is rendered at the
          bottom of the General tab, which is the first thing in the rail and
          the place somebody looks for a version number. Exported from this file
          rather than moved, because the two buttons and the mail address it
          carries belong with the help text that explains them. */}
    </div>
  );
}

const REPO_URL = 'https://github.com/junkerderprovinz/knightloader';
const CONTACT_MAIL = 'hello@halleluja.design';
const GLIMSTONE_URL = 'https://github.com/junkerderprovinz/glimstone';
/**
 * The PayPal.Me page, read off the one place this repository already publishes
 * it: README.md's donate row, which links https://paypal.me/hallelujadesign
 * twice - once in the header and once in the support section near the end -
 * and both times with the "live" button image rather than a placeholder.
 *
 * IT STOOD EMPTY HERE, under a comment saying the page did not exist yet, so
 * the button below never rendered once although its mark, its brand class and
 * its translated label were all finished. The card's rule is never to offer a
 * control that reaches nowhere (GlimStone 1.9.0/1.10.0, which is also where
 * three give routes in one row come from); the mirror of that rule is that a
 * route the product advertises on its own front page is one the card owes the
 * reader. A PayPal.Me link is created once and cannot be renamed afterwards
 * without asking their support, which is why it is taken from the published
 * one rather than invented here.
 *
 * Typed as `string` rather than inferred, so an empty value stays a case this
 * file handles - no page, no button - and not a constant the compiler folds
 * away.
 */
const PAYPAL: string = 'https://paypal.me/hallelujadesign';
/**
 * The coffee handle, from that same donate row in README.md, so one place in
 * the product knows it (GlimStone 1.7.0: the give button carries the funding
 * handle the repository already publishes).
 *
 * NOT from .github/FUNDING.yml, which is what this line used to claim: that
 * file carries a `github:` entry and one `custom:` link to the
 * more-ways-to-support page, and has never had a `buy_me_a_coffee:` line in
 * it. The handle was right and its stated source was not, which is precisely
 * why nobody noticed - a wrong provenance note only costs anything on the day
 * somebody goes to the named file to change the value.
 */
const COFFEE_URL = 'https://buymeacoffee.com/junkerderprovinz';

/** The About card's controls, dressed identically. One constant rather than the
 *  same forty characters repeated per link: controls that are supposed to be
 *  one object should not be five chances to drift.
 *
 *  `glim-brand-btn` rides in here because every one of them wears a mark and
 *  every mark takes its colour the same way (GlimStone 1.10.0): the adjusted
 *  value at rest, the true colour as the hover fill, that fill's ink on top.
 *  WHICH brand is named at each call site with its own `glim-brand-<name>`,
 *  because a mark is passed explicitly and is never reachable by pattern. */
const ABOUT_BTN =
  'glim-brand-btn inline-flex items-center justify-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2' +
  ' px-3.5 py-2 text-sm font-medium text-carbon-text transition duration-150 select-none' +
  ' hover:bg-carbon-surface3 motion-safe:active:scale-[.98]';

/**
 * The tag behind a running version string, derived exactly the way GlimStone's
 * own card derives it (reference/react/AboutCard.tsx): drop the build metadata
 * that semver puts after a '+', and add a leading 'v' ONLY where the stamp does
 * not already carry one.
 *
 * That second half is the whole reason this function exists here. This build
 * stamps the number WITH the v: internal/buildinfo/buildinfo.go is filled by
 * `-X ...buildinfo.Version=vX.Y.Z`, internal/api/routes_system.go hands it to
 * /api/health untouched, and .github/workflows/release.yml passes
 * VERSION=${{ github.ref_name }} from tags named v1.0.0. The footer used to
 * build `/releases/tag/v${version}` from that, so every released build linked
 * .../tag/vv1.0.0 - a 404 on the one link the card exists to offer, and one
 * that no dev build could ever show, because a dev build takes the other
 * branch.
 */
export function releaseTag(version: string): string {
  const bare = version.split('+')[0].trim();
  if (!bare) return '';
  return bare.startsWith('v') ? bare : `v${bare}`;
}

/** A published release, and nothing else, earns a link. GlimStone requires the
 *  shape to be checked BEFORE an anchor exists at all: a link is only a link if
 *  something is behind it, and a pre-release, a branch stamp or the documented
 *  preview image (`--build-arg VERSION=preview`) has no release page to reach. */
const RELEASE_TAG = /^v\d+\.\d+\.\d+$/;

/**
 * One version number in the footer: a link where the build is a released tag,
 * plain text where it is not.
 *
 * The number is shown WITHOUT its leading 'v' - the word in front of it already
 * says "Version" - while the link carries the tag with it, because that is what
 * the tag is called on GitHub. New tab: leaving Settings to read a changelog is
 * not what anybody meant by clicking a number.
 *
 * MUTED INK AND NO UNDERLINE, WHICH REVERSES WHAT THIS FILE USED TO DO. It
 * carried `text-accentInk hover:underline`, commented as a deliberate
 * departure, and the departure does not survive being looked at across the
 * colour modes: --color-accentInk follows the accent in dark, light and system,
 * and takes a per-card hue in rainbow, so the footer rendered as two brightly
 * coloured numbers inside one grey line - the reactive mode was the only one
 * that landed on the language's own answer, and only by accident, because it
 * rebinds --accent-ink to the muted tone anyway. GlimStone's rule
 * (`text-carbon-textMuted no-underline hover:text-carbon-text`) keeps the line
 * one line: the affordance is the ink lifting under the pointer, which is
 * enough for a number nobody is hunting for, and it is the same in every theme,
 * every rainbow and every shape mode because --carbon-text and
 * --carbon-text-muted are defined in all three theme blocks and rebound by
 * none of the colour modes. No exception is recorded here, deliberately: a
 * documented exemption looks exactly like a forgotten control to the next
 * person reading the card.
 */
function VersionNumber({
  version,
  repo,
  unreleased,
}: {
  version: string;
  repo: string;
  unreleased: string;
}) {
  // No stamp at all - the health call has not answered yet, or failed, or a
  // server answered without the field - and 'dev', which is what buildinfo.go
  // holds for an untagged local or main build. Neither is a number anybody
  // could quote, so the working title stands in, exactly as it always has here.
  // Checked BEFORE the tag is derived, so a missing field cannot take the card
  // down on its way through releaseTag.
  if (!version || version === 'dev') return <>{unreleased}</>;
  const tag = releaseTag(version);
  // A real stamp with no release behind it - the documented preview image is
  // the case that exists (`--build-arg VERSION=preview`), a pre-release tag the
  // case that could. It gets no link, and it is shown exactly as the build
  // wrote it rather than hidden behind the working title: this string is what
  // somebody puts in a bug report, and the working title says nothing.
  if (!RELEASE_TAG.test(tag)) return <>{version}</>;
  return (
    <a
      href={`${repo}/releases/tag/${encodeURIComponent(tag)}`}
      target="_blank"
      rel="noreferrer noopener"
      className="text-carbon-textMuted no-underline hover:text-carbon-text"
    >
      {version.replace(/^v/, '')}
    </a>
  );
}

/**
 * Which GlimStone this surface is built against. Kept in step by hand, because
 * the design language is a document plus a stylesheet rather than a package.
 *
 * THE THREE COPIES NO LONGER AGREE, AND THAT IS DELIBERATE. The same constant
 * lives in the extension's options.js and the app's SettingsScreen, and until
 * 2026-09-13 all three said 1.6.0. The web UI has since been lifted through
 * 1.9.0 to 1.17.0: the scrim is a token, nothing destructive is painted red
 * (ButtonKind has no 'danger' left to pass), the control that goes ahead sits at
 * the end of its row, the brand marks carry their own colours, and the top
 * motion intensity travels and springs instead of running the quiet shape
 * faster. Then 1.15.0's second way in (a second factor and passkeys, each a
 * card that a login GAINS rather than one that replaces it, with a refusal in
 * prose where the environment forbids the exchange), 1.16.0's answer to the two
 * rules that had contradicted each other for six releases (a control whose
 * value still ACTS stays and dims, one with nothing behind it goes - the accent
 * row and eight others were sorted by that question, and
 * web/check-dimmed-and-inert.mjs now holds the line), and 1.17.0's hidden
 * fourth motion level with the rule it establishes for any easter egg that
 * changes behaviour. The extension and the app say 1.17.0 (the clause here said
 * 1.14.0 and had been left behind by their own sweeps); neither has had a pass
 * since, and both now carry a number the release list does not have - see the
 * fold below, which is a defect on those two surfaces and not this one's to fix.
 *
 * THE ONE THING THAT NUMBER WAS AHEAD OF IS DONE, AND IT IS WHAT 2.0.0 IS FOR.
 * The note that stood here said the top motion level had been renamed from
 * "full" to "wild" while this surface still called it "Voll" - a stored value
 * plus a label in 42 catalogues, so the rename was its own piece of work. It is
 * that work that 2.0.0 is a MAJOR release for: `wild` is what goes into
 * data-motion, what index.css matches on and what sits in localStorage, so an
 * app keeping the old spelling speaks a different wire format from the language
 * it claims and from its own phone app, which had already moved. Done in this
 * round: the type, the two lists, the default, the attribute, every comment that
 * named the old value, the key in all 42 catalogues and the word behind it - in
 * the phone's catalogues too, so both surfaces say one thing. The saved value is
 * TRANSLATED on the way in (MIGRATED_MOTION in lib/appearance.ts) rather than
 * dropped, because a value that falls through to the default is a setting that
 * looks like it forgot itself. web/check-motion-top-level.mjs holds all of it,
 * and measures the migration by importing the module rather than reading it.
 *
 * So this number is per SURFACE, not per repository, and raising the other two
 * to match would be the one thing worse than them differing: a card claiming a
 * release its own files do not speak. They move when their sweep runs.
 *
 * AND THE NUMBER THAT STOOD HERE WAS A 404, which is the failure this card's own
 * rule was written about. GlimStone FOLDED its release history at 2.0.0: the
 * editions this comment keeps naming - 1.10.0 through 1.19.0 - were never cut as
 * releases and were rolled into 2.0.0 instead, so the published list is v1.0.0
 * to v1.9.0, then v2.0.0 and v2.1.0. Every version on this card is a LINK to its
 * own release page, so "1.18.0" did not merely read wrong: measured on
 * 2026-09-14, .../releases/tag/v1.18.0 answered 404 and v2.1.0 answered 200. The
 * earlier editions named above are kept in this comment as the record of what
 * was done when; as VERSIONS they no longer exist, and the only number that may
 * stand below is one the release list has.
 *
 * AND THAT SWEEP SKIPPED TWO EDITIONS ON THIS SURFACE. 1.7.0 and 1.8.0 were
 * never applied here: a provenance note on a copied component ("1.8.3") was
 * read as this app's own version claim, so the run started at 1.9.0 and the two
 * below it were taken for done. What 1.7.0 asks of THIS file - a tag derived
 * from the stamp rather than glued to it, and the footer number in the muted
 * ink - is in as of this round; whatever else those two editions want lives in
 * other files and moves with them. Which is the standing rule for the number
 * below, and the one this constant keeps breaking: it rises when an edition is
 * really through, not when a round that meant to do it ends.
 *
 * 1.18.0 is a one-rule edition and it came FROM here: the second-factor card
 * shipped titled "Zweiter Faktor", jdp reported it, and the language turned out
 * to describe everything those two cards must do and never what they are
 * called. The rule now lives in GlimStone ("a card that offers a named,
 * established capability carries that capability's name"), and this surface
 * satisfies it - auth.twoFactor.title is the established phrase in all 42
 * catalogues, looked up per language rather than translated from the English.
 *
 * 2.0.0 IS THE MOTION RENAME ABOVE, plus the rest of that edition, and the rest
 * of it was already here or already true. Checked one by one rather than
 * assumed, because this constant's own standing rule is that it rises when an
 * edition is really through: the scrim is a token at .65/.55, the soft warn fill
 * exists in all three theme blocks and the refusal box has a real ground, the
 * toast travels 24/12/0 with its RTL twin coming from --dir-sign, the enrolment
 * cards carry all four of what an enrolment owes, nothing destructive is red and
 * no ConfirmDialog `tone` survives to be dropped (there is none, and ContextMenu
 * has no `danger` either). Three of that edition's rules were verified as
 * already-correct rather than changed, and each is worth naming because "no diff"
 * and "not looked at" are the same commit otherwise. A rule must name the QUIET
 * motion levels: this stylesheet has no rule that names a lively one at all,
 * because the top level's block IS the bare :root - correct by construction, and
 * guarded now so it stays that way. A sentence that FOLLOWS controls takes a
 * step of space above it: the About card below is the only card here that runs
 * sentence-controls-sentence-controls, and it has carried that step since
 * 2026-09-06. The give buttons run hosted-page first, wallet last: coffee,
 * PayPal, the crypto window, in that order in the JSX. `IconSave` is Vecteezy's
 * drawing in every app that speaks this language - this one has no save glyph at
 * all, so there is nothing to align; its save controls are worded buttons. And
 * the glyph-crop rule was measured rather than eyeballed: all 60 glyphs in
 * lib/icons.tsx, getBBox() on live markup, ink between 55% and 100% of the
 * 20-unit grid with the bulk between 68% and 75%, so none of them is the
 * three-quarter outlier that rule is about.
 *
 * 2.1.0 is two rules, and the first of them is this surface's own report again.
 * The two cards that enrol a login control said different things - "Einrichten"
 * on the second factor and "Passkey hinzufügen" on the passkey card - for one
 * act, so both now say this app's own word for starting a setup, looked up per
 * language from auth.twoFactor.enable rather than translated afresh, and each
 * wears the glyph of its own capability: a shield with a check, and a plus.
 * web/check-enrolment-buttons.mjs holds the two to one word in all 42
 * catalogues. The other rule is 22, a card CONFIGURES and the shell OPERATES:
 * gone through here and nothing to move. The password card lost its sign-out on
 * 2026-09-07, before the rule existed; the one card on these pages that is
 * nothing but operations (quit and restart) configures nothing, and rule 22's
 * own test - where does the capability live once the button is gone - answers
 * "nowhere", so it stays.
 */
const GLIMSTONE_VERSION = '2.1.0';

/**
 * The About card (jdp, 2026-08-31: "in der App und der Erweiterung und im KL
 * soll eine neue Card rein ... darin sollen die versionsnummern stehen und ein
 * text ... Die vversionsnummer sollen dann nicht nochmal unter den card im
 * hintergrund angeziegt werden").
 *
 * It replaces the fixed, centred version line that used to sit at the bottom of
 * every settings tab. That line was GlimStone's own answer until now, and its
 * weakness only shows once you ask what somebody does NEXT with a version
 * number: they report something. Page chrome has nowhere to put that, a card
 * does, and the two buttons turn "report it" from a search into a click.
 *
 * On the Help page rather than a page of its own, because this is already the
 * destination for "something is not working and I do not know where to look",
 * and one more entry in the settings rail for two sentences and two links would
 * be a tile nobody visits on purpose. hue 11 continues this page's own run.
 */
/**
 * THE CREST, AND THE COMMIT ON ITS BACK (docs/easter-eggs.md).
 *
 * Press and hold: it turns, and the back carries the revision this build was
 * made from. Let go and it turns back. Nothing is stored, nothing is announced,
 * and no settings list gains an entry - which is the rule every egg in this app
 * follows and the reason the list exists.
 *
 * IT ONLY TURNS WHEN THERE IS SOMETHING TRUE ON THE BACK. With no revision -
 * an older server that does not send the field, or a build made where nothing
 * stamped one (buildinfo.Revision spells out both) - this renders the crest as
 * a plain mark with no press behaviour at all, rather than turning to show a
 * dash or the word "unknown". A back with a made-up identifier on it is worse
 * than no back: the whole excuse for putting real information inside a joke is
 * that somebody hunting a stale deploy can read it, and a plausible wrong answer
 * is the one outcome that leaves them worse off than silence.
 *
 * SEVEN CHARACTERS, and NOT because that is what git prints. An earlier version
 * of this note claimed the seven matched `git log --oneline`; measured in this
 * repository both that and `git rev-parse --short HEAD` print EIGHT
 * (7b3546ba), because git's short form is adaptive - it grows with the history
 * until a prefix is unambiguous, so it is a moving number and a different one
 * per clone. Seven is the fixed form: it is what GitHub's own commit UI shows,
 * it is a valid prefix for `git show` and `git checkout`, and pinned here it
 * means the back of the crest is the same width in every build of every clone
 * for the rest of the app's life. Not the full forty, and not in a native
 * `title` either: the house rule is that an explanation is an info bubble and
 * never the operating system's own box, and a bubble here would be a label
 * announcing the secret it is hiding.
 *
 * THE BACK IS ORDINARY TEXT IN THE DOCUMENT AT ALL TIMES, and that is a
 * decision rather than an oversight. Measured: Ctrl+F finds the revision and a
 * select-all copies it, with nobody having pressed anything. Rendering it only
 * while turned would take that away, and the thing it would take it away from
 * is the only non-pointer route there is: this element is aria-hidden with no
 * keyboard path by design (see below), so find-in-page is how somebody who
 * cannot press and hold reads the revision at all. There is also nothing here
 * worth hiding - /api/health serves the same string to anyone who can open this
 * page, and the version line one row down is the public answer to "which build
 * is this". The egg is the GESTURE, not the datum, and a joke that made the
 * datum harder to reach would be paying for the joke with the information.
 *
 * IT IS DECORATION IN THE ACCESSIBILITY TREE AND THAT IS DELIBERATE, the same
 * call the blade in the sidebar makes (Sidebar.tsx's useDrawAndStrike). Giving
 * it a role and a name would mean writing that name in 42 catalogues, and every
 * one of those sentences would have to either describe the gesture - which
 * hands the surprise to the one reader who never looks - or say nothing useful
 * at all. What is NOT lost by this is the information: the version number a few
 * lines down is the answer to "which build is this" for everybody, and the
 * revision is the extra half a step for somebody already standing in front of a
 * deploy they do not trust.
 */
function Crest({ commit }: { commit: string }) {
  const [turned, setTurned] = useState(false);
  const mark = <IconCrest width={34} height={34} className="shrink-0 text-carbon-textMuted" />;
  // No revision, no back, and no press behaviour either - see the note above on
  // why a back with nothing true on it is worse than a crest that only sits
  // there. This is the ordinary state on any server older than this field.
  if (!commit) return mark;
  return (
    <span
      className="kl-crest shrink-0"
      data-turned={turned ? 'yes' : 'no'}
      aria-hidden
      onPointerDown={() => setTurned(true)}
      onPointerUp={() => setTurned(false)}
      onPointerLeave={() => setTurned(false)}
      onPointerCancel={() => setTurned(false)}
    >
      <span className="kl-crest-turn">
        <span className="kl-crest-face">{mark}</span>
        {/* dir="ltr": a hexadecimal revision is one token and must not be
            reordered in an Arabic or Hebrew locale, the same rule the speed
            figure in the shell strip carries. */}
        <span
          dir="ltr"
          className="kl-crest-face kl-crest-back glim-num text-[11px] font-semibold leading-none text-carbon-textSub"
        >
          {commit.slice(0, 7)}
        </span>
      </span>
    </span>
  );
}

export function About({ hue }: { hue: number }) {
  const { t } = useT();
  const [version, setVersion] = useState('');
  const [commit, setCommit] = useState('');
  const [cryptoOpen, setCryptoOpen] = useState(false);
  useEffect(() => {
    fetchHealth()
      .then((h) => {
        setVersion(h.version);
        // ?? '', not a fallback string: see the Crest above on why an absent
        // revision has to stay absent all the way down.
        setCommit(h.commit ?? '');
      })
      .catch(() => {});
  }, []);
  return (
    <Card hue={hue} className="flex flex-col gap-3">
      <SectionTitle>{t('settings.about.title')}</SectionTitle>
      <p className="text-sm text-carbon-textSub">{t('settings.about.body')}</p>
      {/* Three sentences, each with the thing it asks for directly under it
          (jdp, 2026-09-01). The order is his: what this is, then the coffee,
          then the way to report something. A sentence with its own button under
          it reads as one offer; three sentences stacked over one row of buttons
          reads as a form. */}
      <p className="text-sm text-carbon-textSub">{t('settings.about.coffee')}</p>
      {/* Up to three ways to give, and they are separate because they reach
          different people: the coffee and PayPal take a card or a balance, the
          crypto window takes what somebody already holds in a wallet and shows
          no name at either end. All of them stay in THIS row rather than
          getting a row of their own further down, which is the card's own rule
          - a sentence sits directly above the thing it asks for, and a second
          row reads as a second, unrelated offer.

          ORDER: the hosted payment pages first, the wallet last (GlimStone
          1.10.0). It reads as a ramp rather than an alphabet - the routes most
          people already hold an account for, then the one that needs none and
          shows no name at either end. */}
      <div className="flex flex-wrap gap-2">
        <a
          href={COFFEE_URL}
          target="_blank"
          rel="noreferrer noopener"
          className={`${ABOUT_BTN} glim-brand-coffee`}
        >
          <IconBuyMeACoffee size={15} />
          {t('settings.about.coffeeButton')}
        </a>
        {PAYPAL !== '' && (
          <a
            href={PAYPAL}
            target="_blank"
            rel="noreferrer noopener"
            className={`${ABOUT_BTN} glim-brand-paypal`}
          >
            <IconPayPal size={15} />
            {t('settings.about.paypal')}
          </a>
        )}
        {/* A real button rather than an anchor dressed as one, unlike its two
            neighbours: this one opens a window in the app instead of going
            somewhere, so there is no link for the browser's middle click,
            copy-link or open-in-new-tab to act on.

            ITS MARK IS THE BARE LETTERFORM AND IT TAKES A BRAND CLASS, which
            settles a question this note argued the other way for two rounds.
            The old reading was that the coin disc may stay, because the ₿ is
            knocked OUT of a single path rather than painted on top, so the
            class paints the disc without swallowing the symbol. That much is
            true and was measured, and it is not the deciding test. At 15px in
            a row beside a cup and a P, a filled circle with a hole in it hands
            the eye the shape of the GROUND rather than the shape of the
            letter, so the one button that should say "crypto" says "orange
            dot". The letterform says it at any size.

            What the language actually forbids in this row is a mark that
            brings its own ground, and the clearest case is a two-colour coin
            logo: the class paints every path with one ink, so a white symbol
            on a filled circle disappears into the circle. Such a mark belongs
            on the coin tiles in the donation window, where it is large,
            nothing repaints it, and telling eight logos apart is the job.
            check-brand-marks.mjs encodes that test ("no mark drawn in MORE
            THAN ONE COLOUR inside a .glim-brand-btn") and this button passes
            it the easy way now: one path, one colour, no ground. */}
        <button
          type="button"
          className={`${ABOUT_BTN} glim-brand-bitcoin`}
          onClick={() => setCryptoOpen(true)}
        >
          <IconBitcoin size={15} />
          {t('settings.about.crypto')}
        </button>
      </div>
      {cryptoOpen && <CryptoDonateDialog onClose={() => setCryptoOpen(false)} />}
      {/* One extra step of space above this line, and only above this one
          (jdp, 2026-09-06). The card holds two offers, and without the break
          the coffee button sits as close to the next sentence as to the one it
          belongs to, so the eye pairs it with the wrong text. A blank line is
          what separates two paragraphs everywhere else, and that is all this
          is. */}
      <p className="mt-2 text-sm text-carbon-textSub">{t('settings.about.report')}</p>
      <div className="flex flex-wrap gap-2">
        {/* Anchors dressed as buttons rather than buttons that navigate: one
            opens a site and one hands off to a mail client, and both want the
            browser's own middle-click, copy-link and open-in-new-tab. */}
        <a
          href={REPO_URL}
          target="_blank"
          rel="noreferrer noopener"
          className={`${ABOUT_BTN} glim-brand-github`}
        >
          <IconGithub width={15} height={15} />
          {t('settings.about.github')}
        </a>
        {/* THE ONE CONTROL HERE THAT CARRIES NO VENDOR'S MARK, and its class
            says so: `glim-brand-house` reads the accent tokens, so this button
            follows the user's accent and rainbow mode. It reaches the app's own
            authors rather than a third party, and that is the only route that
            may be coloured by the engine - a vendor's mark never is. */}
        <a
          href={`mailto:${CONTACT_MAIL}?subject=${encodeURIComponent(`KnightLoader ${t('settings.about.mailSubject')}`)}`}
          className={`${ABOUT_BTN} glim-brand-house`}
        >
          <IconMail width={15} height={15} />
          {t('settings.about.mail')}
        </a>
      </div>
      {/* Last line in the card, under the buttons (jdp, 2026-09-05). It reads
          as a footer, which is what it is: the sentences above are what the
          card wants to say and each has its own button under it, while a build
          number is what somebody looks up afterwards. Between the body and the
          coffee line it cut that pairing in half.

          Both numbers are LINKS to their own release page (jdp, 2026-08-31:
          "Die Versionsnummer (auch von Glimstone) soll immer auf deren release
          auf github zeigen ... Das soll immmer und überall gelten"). A version
          answers "which build is this"; the question straight after it is
          always "and what changed". GlimStone 1.6.0 makes it the family rule.

          Built from the version, never a hand-kept list of links, and the tag
          is DERIVED rather than glued together (releaseTag above): the check
          used to be "is there a version and is it not 'dev'", which said yes to
          every build that had a stamp at all and then pasted a second 'v' in
          front of a stamp that already carried one. A build with no release
          behind it stays plain text rather than offering a link into a 404, and
          now that includes the ones that are neither empty nor 'dev'. */}
      {/* THE CREST SITS IN THIS ROW AND NOT AT THE HEAD OF THE CARD, which is
          where it was first put and where it could not go. The card's heading is
          a badge the Card itself positions absolutely on its top edge (GlimStone
          1.4.0), so a mark "beside the title" is a mark alone on a line under a
          floating badge - measured on screen, not deduced.

          This is the better place anyway, and for the reason the egg exists: the
          back of the crest carries the revision, the line beside it carries the
          version, and the question that makes either of them worth reading is
          "which build am I actually looking at". Two halves of one answer belong
          on one line. */}
      <div className="flex items-center gap-3">
        <Crest commit={commit} />
        <p className="glim-num text-xs text-carbon-textMuted">
          {t('settings.about.version')}{' '}
          <VersionNumber version={version} repo={REPO_URL} unreleased={t('nav.workingTitle')} />
          {' · GlimStone '}
          <VersionNumber
            version={GLIMSTONE_VERSION}
            repo={GLIMSTONE_URL}
            unreleased={t('nav.workingTitle')}
          />
        </p>
      </div>
    </Card>
  );
}

import { type ReactNode, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useT } from '../../lib/i18n';
import { Card, SectionTitle } from '../../components/ui';
import { fetchHealth } from '../../lib/api';
import { IconGithub, IconMail } from '../../lib/icons';
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
 * changes behaviour. The extension and the app stand at 1.14.0; neither has had
 * the last three passes.
 *
 * ONE THING THIS NUMBER IS AHEAD OF, WRITTEN DOWN RATHER THAN LEFT TO BE FOUND:
 * 1.10.0 renamed the top motion level from "full" to "wild", and this surface
 * still calls it "Voll" - a stored value plus a label in 42 catalogues, so the
 * rename is its own piece of work. It was already missing when the number went
 * to 1.14.0. Everything else those editions ask of this surface is in.
 *
 * So this number is per SURFACE, not per repository, and raising the other two
 * to match would be the one thing worse than them differing: a card claiming a
 * release its own files do not speak. They move when their sweep runs.
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
 */
const GLIMSTONE_VERSION = '1.18.0';

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
export function About({ hue }: { hue: number }) {
  const { t } = useT();
  const [version, setVersion] = useState('');
  const [cryptoOpen, setCryptoOpen] = useState(false);
  useEffect(() => {
    fetchHealth()
      .then((h) => setVersion(h.version))
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

            Its mark is the flat Bitcoin symbol rather than a coin disc, so it
            takes a brand colour like the two beside it. The language's own card
            leaves this one button unclassed because the disc IT draws carries
            its own ground, and the contrast that decides whether such a mark
            can be read sits inside the drawing. */}
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
    </Card>
  );
}

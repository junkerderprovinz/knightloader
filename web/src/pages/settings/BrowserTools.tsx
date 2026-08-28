import { useEffect, useState, type ReactNode } from 'react';
import logoUrl from '../../assets/logo.svg';
import { buildBookmarklet } from '../../lib/browserTools';
import { fetchExtensionVersion } from '../../lib/api';
import { useInstallPrompt } from '../../lib/pwaInstall';
import { useT } from '../../lib/i18n';
import { Button, Card, InfoBubble, SectionTitle } from '../../components/ui';

/**
 * Every way to reach KnightLoader from outside the app itself: a bookmarklet,
 * the MV3 browser extension (extension/src), and the native apps.
 *
 * The extension download used to be built per instance, with this instance's
 * address baked into a config.default.json. It is now byte-identical to the
 * source (internal/api/routes_browsertools.go), because the extension is set
 * up with the connection phrase and holds no addresses at all - which is also
 * what makes a store package reproducible from a checkout.
 *
 * The app card moved here from the Zugang tab (jdp, 2026-08-27: "Die App card
 * dann bitte in den Browser-Werkzeuge verschieben und den Tab Browser & App
 * benennen"), and it took its keys with it: it lived under
 * settings.access.remote.* while it sat on that page, and a key named after
 * the page it used to be on is the kind of thing nobody dares delete two
 * waves later. The rail label follows the same move - one tab that answers
 * "how do I get at this from somewhere else", whether that somewhere is a
 * browser or a phone.
 *
 * The bookmarklet and the PWA share target land on /quickadd (pages/
 * QuickAdd.tsx), so this page only ever has to build the address and the
 * drag-target, not the staging logic. The extension no longer does: it carries
 * its own relay client and posts to /api/links through the group, which is the
 * only way to reach an instance that has no address to open a window at.
 */
export function BrowserTools() {
  const { t } = useT();
  const origin = window.location.origin;
  const bookmarklet = buildBookmarklet(origin);
  const [copied, setCopied] = useState(false);
  const [extensionVersion, setExtensionVersion] = useState<string | null>(null);
  useEffect(() => {
    void fetchExtensionVersion()
      .then((v) => setExtensionVersion(v.version))
      .catch(() => {});
  }, []);

  const chromiumHint = (
    <ol className="list-decimal space-y-1 pl-4">
      <li>{t('settings.browsertools.installChromiumStep1')}</li>
      <li>{t('settings.browsertools.installChromiumStep2')}</li>
      <li>{t('settings.browsertools.installChromiumStep3')}</li>
      <li>{t('settings.browsertools.installChromiumStep4')}</li>
    </ol>
  );
  const firefoxHint = (
    <ol className="list-decimal space-y-1 pl-4">
      <li>{t('settings.browsertools.installFirefoxStep1')}</li>
      <li>{t('settings.browsertools.installFirefoxStep2')}</li>
      <li>{t('settings.browsertools.installFirefoxStep3')}</li>
    </ol>
  );
  const installLabel = t('settings.browsertools.installLabel');

  return (
    <div className="flex flex-col gap-10">
      <Card className="flex flex-col gap-3">
        <SectionTitle hue={0}>{t('settings.browsertools.bookmarkletTitle')}</SectionTitle>
        {/* A real numbered list, not a flowing paragraph - jdp: "Bitte
            aufzählungen immer untereinander", the same rule the install
            steps below now follow too. */}
        <ol className="list-decimal space-y-1.5 pl-4 text-sm text-carbon-textSub">
          <li>{t('settings.browsertools.bookmarkletStep1')}</li>
          <li>{t('settings.browsertools.bookmarkletStep2')}</li>
        </ol>
        <div className="flex flex-wrap items-center gap-3">
          {/* A real link, not a button with an onClick — dragging IS the
              install step, and only an <a href="javascript:..."> is
              draggable into a bookmarks bar carrying that code. The icon is
              KnightLoader's own app mark (jdp: "Das lesezeichen soll das
              logo als icon anzeigen"), not a generic download glyph -
              dragging this in is dragging in KnightLoader itself. */}
          <a
            href={bookmarklet}
            onClick={(e) => e.preventDefault()}
            className="inline-flex items-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2
              px-3.5 py-2 text-sm font-medium text-carbon-text hover:bg-carbon-surface3"
          >
            <img src={logoUrl} alt="" aria-hidden className="h-5 w-auto shrink-0" />
            {t('settings.browsertools.bookmarkletLink')}
          </a>
          {'clipboard' in navigator && (
            <Button
              kind="ghost"
              className="px-2.5 text-xs"
              onClick={async () => {
                await navigator.clipboard.writeText(bookmarklet);
                setCopied(true);
                setTimeout(() => setCopied(false), 1800);
              }}
            >
              {copied ? t('settings.browsertools.copied') : t('settings.browsertools.copyCode')}
            </Button>
          )}
        </div>
      </Card>

      <Card className="flex flex-col gap-4">
        <SectionTitle
          hue={1}
          right={extensionVersion && <span className="glim-num text-[11px] text-carbon-textMuted">v{extensionVersion}</span>}
        >
          {t('settings.browsertools.extensionTitle')}
        </SectionTitle>
        {/* One badge per browser, each the real brand mark (Simple Icons,
            taken 1:1 - never hand-rebuilt) instead of GlimStone's own
            monochrome glyph set: a Firefox logo re-tinted to the accent
            colour would stop reading as Firefox, so these sit outside the
            rainbow/accent system on purpose, same as any other semantic
            colour (see e.g. a delete action staying red under any accent).
            Chrome/Edge/Brave/Opera/Vivaldi are the same .zip and the same
            install flow (Chromium's own Developer Mode + Load unpacked) -
            one shared instruction bubble, not five copies of it. Firefox
            gets the .xpi and its own bubble (routes_browsertools.go's own
            doc comment explains why the archive itself is identical either
            way). Each bubble sits in its badge's own corner (jdp: "das i
            icon soll in die ecke der jeweiligen downloadcard") rather than
            as a paragraph below the row. */}
        <div className="flex flex-wrap gap-3">
          <DownloadTile logo={<LogoChrome />} name="Chrome" onClick={badgeAction('Chrome', downloadZip)} hint={chromiumHint} hintLabel={installLabel} />
          <DownloadTile logo={<LogoEdge />} name="Edge" onClick={badgeAction('Edge', downloadZip)} hint={chromiumHint} hintLabel={installLabel} />
          <DownloadTile logo={<LogoBrave />} name="Brave" onClick={badgeAction('Chrome', downloadZip)} hint={chromiumHint} hintLabel={installLabel} />
          <DownloadTile logo={<LogoOpera />} name="Opera" onClick={badgeAction('Chrome', downloadZip)} hint={chromiumHint} hintLabel={installLabel} />
          <DownloadTile logo={<LogoVivaldi />} name="Vivaldi" onClick={badgeAction('Chrome', downloadZip)} hint={chromiumHint} hintLabel={installLabel} />
          <DownloadTile logo={<LogoFirefox />} name="Firefox" onClick={badgeAction('Firefox', downloadXpi)} hint={firefoxHint} hintLabel={installLabel} />
        </div>
      </Card>

      <AppCard />
    </div>
  );
}

/**
 * The native apps, and installing this page itself as the smaller second
 * option below them.
 *
 * The three tiles are DownloadTile, the same component the six browsers above
 * use - see its own doc comment. They briefly carried jdp's two official store
 * badges instead (2026-08-27: "auf dem Desktop sind zwei svg dateien. die
 * Buttons bitte in die card einpflegen"); that shape is gone, and with it the
 * altered artwork Google's guidelines would not have allowed.
 *
 * No "not published yet" note anywhere on this card, deliberately (jdp, same
 * message: "kein hinweis im UI. KL wird erst veröffentlicht wenn alles fertig
 * ist"): by the time anybody who is not jdp sees this page, the listings
 * exist and the URLs below are filled in.
 */
function AppCard() {
  const { t } = useT();
  const { available: canInstall, promptInstall } = useInstallPrompt();
  // Safari (desktop and iOS) never fires beforeinstallprompt, so
  // useInstallPrompt's `available` is permanently false there - this is the
  // one place that still has something useful to say instead of nothing: the
  // manual Share-sheet route, iOS's only way to install any web app.
  const iOS = /iphone|ipad|ipod/i.test(navigator.userAgent);

  return (
    <Card className="flex flex-col gap-4">
      {/* The app's own version, beside the title exactly as the extension card
          carries the extension's (jdp, 2026-08-27: "Die Versionsnummer bitte
          auch in der App card anzeigen"). Read from mobile/app.json at build
          time, never typed here - see vite.config.ts for why that distinction
          is load-bearing rather than tidy. */}
      <SectionTitle
        hue={3}
        hint={t('settings.browsertools.appBody')}
        right={<span className="glim-num text-[11px] text-carbon-textMuted">v{__MOBILE_VERSION__}</span>}
      >
        {t('settings.browsertools.appTitle')}
      </SectionTitle>
      {/* Literally the same component as the browser downloads above (jdp,
          2026-08-27: "Kannst du die App download buttons und die der
          browsererweiterung gleich machen?", then again after the first cut:
          "die jetzigen buttons leuchten nicht auf beim mouseover"). Not a
          matching copy - one Tile, used twice - because a shape that has to be
          kept in step by hand is the shape that drifts.

          That replaced jdp's two store badges, and it settles a real problem
          with them: Google's badge guidelines forbid altering their artwork,
          and fitting a wide wordmark plate into this card meant altering it.
          A brand glyph with the store's name under it is the app's own button,
          not a modified official badge - and it is the same object the six
          browsers already are.

          There is deliberately no iOS equivalent of the APK: Apple has no
          sideloading. Without a store listing the only routes are TestFlight
          (a beta programme, 90 days, needs its own developer account) or the
          EU's alternative marketplaces, and neither is a file somebody
          downloads from here. The card's own hint says so rather than leaving
          a conspicuous gap unexplained. */}
      <div className="flex flex-wrap items-center gap-3">
        <DownloadTile
          logo={<BrandMark svg={PLAY_SVG} />}
          name={t('settings.browsertools.storeAndroid')}
          onClick={openIfSet(APP_URLS.android)}
        />
        <DownloadTile
          logo={<BrandMark svg={APPLE_SVG} />}
          name={t('settings.browsertools.storeIOS')}
          onClick={openIfSet(APP_URLS.ios)}
        />
        {/* The APK gets KnightLoader's own mark (jdp: "Bitte auch ein Logo für
            die APK Card") - the other two tiles wear the shop's logo because
            the shop is what you are being sent to, and this one sends you to
            the app itself. */}
        <DownloadTile
          logo={<img src={logoUrl} alt="" aria-hidden className="h-full w-full object-contain" />}
          name={t('settings.browsertools.apkLabel')}
          onClick={() => window.open(APP_URLS.apk, '_blank', 'noopener,noreferrer')}
        />
      </div>
      {(canInstall || iOS) && (
        <div className="flex flex-col gap-2 pt-1">
          <span className="text-xs font-semibold text-carbon-textSub">
            {t('settings.browsertools.installPwaLabel')}
          </span>
          {canInstall && (
            <div>
              <Button kind="secondary" hue={3} onClick={() => void promptInstall()}>
                {t('settings.browsertools.install')}
              </Button>
            </div>
          )}
          {!canInstall && iOS && (
            <p className="text-[11px] text-carbon-textMuted">{t('settings.browsertools.installIOS')}</p>
          )}
        </div>
      )}
    </Card>
  );
}

/**
 * Where the three app buttons go. The two store URLs are empty until a
 * listing actually goes live (jdp is filling them in once the apps are
 * published) - same arrangement as STORE_URLS above for the extension, and
 * the same reason it is a constant rather than a setting: which store a
 * release exists in is a fact about the build, not about this instance.
 *
 * The APK link points at the releases INDEX rather than /releases/latest,
 * which would be wrong the moment a server release outranks a mobile one -
 * the mobile builds carry their own mobile/vX.Y.Z tags, and GitHub has no way
 * to ask for "the newest release whose tag starts with mobile/".
 */
const APP_URLS = {
  android: '',
  ios: '',
  apk: 'https://github.com/junkerderprovinz/knightloader/releases',
};

/**
 * openIfSet builds the click handler for a store tile whose listing does not
 * exist yet.
 *
 * The tile stays fully alive - same surface, same white hover - rather than
 * rendering `disabled` the way it used to. That is jdp's own call twice over:
 * no "not published yet" note anywhere on this card (2026-08-27: "kein hinweis
 * im UI"), and the buttons must light up under the pointer like every other
 * download here ("die jetzigen buttons leuchten nicht auf beim mouseover") -
 * a `disabled` button is exactly what was swallowing that hover. Until a URL
 * lands in APP_URLS the click simply does nothing, which nobody but jdp will
 * ever be in a position to notice.
 */
function openIfSet(url: string): () => void {
  return () => {
    if (url) window.open(url, '_blank', 'noopener,noreferrer');
  };
}

/**
 * One download target: a square tile carrying a mark and a name, and nothing
 * else. The tile itself is the button.
 *
 * This is the ONE download shape on the page - the six browsers, the two
 * stores and the APK are all this component (jdp, 2026-08-27: "Kannst du die
 * App download buttons und die der browsererweiterung gleich machen?"). They
 * are the same act, so they are the same object; the App card used to build
 * its own wider variant and that alone made a store listing read as a
 * different KIND of thing from an extension download.
 *
 * An optional `hint` renders as an (i) bubble pinned to the tile's own
 * top-right corner, as a sibling of the button rather than a child of it -
 * InfoBubble is its own focusable, hoverable element, and nesting it inside
 * the button would make a click on the (i) also fire the download underneath.
 *
 * Vendor marks keep their own colours through the hover; only the surface and
 * the caption move. The Apple mark is the exception and does so deliberately -
 * it is monochrome by definition, and Apple's own guidance is dark-on-light,
 * light-on-dark, which is what inheriting `currentColor` gets.
 *
 * The hover goes light in the dark theme (jdp: "Beim mouseover soll der
 * hintergrund weiß werden") and DARK in the light one. Not a second opinion
 * about the request - white on white is not a hover at all: these tiles sit on
 * a card that is already #ffffff in the light theme, so a literal white hover
 * made the tile vanish into the card instead of lifting off it. Measured, not
 * assumed. What jdp asked for is a step away from the surface, and this is
 * that step in both directions.
 */
const tileClass =
  'flex flex-col items-center justify-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 ' +
  'text-carbon-text transition-colors duration-150 ' +
  'hover:bg-carbon-surface3 dark:hover:bg-white dark:hover:text-[#161616]';

function DownloadTile({
  logo,
  name,
  onClick,
  hint,
  hintLabel,
}: {
  logo: ReactNode;
  name: string;
  onClick: () => void;
  hint?: ReactNode;
  hintLabel?: string;
}) {
  return (
    <div className="relative">
      <button type="button" onClick={onClick} title={name} aria-label={name} className={`${tileClass} h-28 w-28`}>
        <span className="flex h-14 w-14 shrink-0 items-center justify-center">{logo}</span>
        <span className="text-xs font-medium">{name}</span>
      </button>
      {hint && (
        <span className="absolute right-1.5 top-1.5">
          <InfoBubble tip={hint} label={hintLabel} />
        </span>
      )}
    </div>
  );
}

/**
 * Filled in once a listing actually goes live in that store - empty for now,
 * since submission needs jdp's own developer accounts (Chrome Web Store,
 * Microsoft Partner Center, addons.mozilla.org) and none exist yet. Brave,
 * Opera and Vivaldi install straight from the Chrome Web Store rather than
 * running their own listing, so they share Chrome's URL once it exists.
 * Until a URL is set here, that badge keeps downloading the packaged
 * extension directly - see badgeAction() below.
 */
const STORE_URLS: Record<'Chrome' | 'Edge' | 'Firefox', string> = {
  Chrome: '',
  Edge: '',
  Firefox: '',
};

/** badgeAction opens the live store listing once one exists, otherwise falls
 *  back to downloading the packaged extension straight from this instance. */
function badgeAction(store: 'Chrome' | 'Edge' | 'Firefox', fallback: () => void): () => void {
  const url = STORE_URLS[store];
  return url ? () => window.open(url, '_blank', 'noopener,noreferrer') : fallback;
}

function downloadZip() {
  window.location.href = '/api/browser-extension.zip';
}

function downloadXpi() {
  window.location.href = '/api/browser-extension.xpi';
}

/**
 * Real brand marks, jdp's own SVGs (Chrome/Edge/Brave/Opera/Vivaldi from
 * each vendor's own brand assets, Firefox from Mozilla's), reproduced whole
 * and unedited - the full official multi-colour artwork, not a Simple
 * Icons-style single-hex silhouette. Rendered via dangerouslySetInnerHTML
 * rather than hand-converted to JSX: these files carry gradients, `<use>`
 * references and kebab-case SVG attributes (stop-color, clip-path, …) that
 * a manual JSX port could silently mistranslate - injecting the markup
 * as-is is the only way to guarantee it renders exactly as given. Every id
 * is prefixed per logo (kl-<name>-*) so six of these sitting in the same
 * page can never collide (two files both defining id="a" would otherwise
 * make one badge borrow the wrong gradient). Kept local to this page: every
 * other icon in lib/icons.tsx is a `currentColor` glyph in the GlimStone
 * style, and these are fixed-colour brand marks that must NOT flow through
 * the accent/rainbow system on purpose (see this file's own top comment).
 */
function BrandMark({ svg }: { svg: string }) {
  return (
    <span
      className="block h-full w-full [&>svg]:block [&>svg]:h-full [&>svg]:w-full"
      aria-hidden
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  );
}

// Google Play's own four-facet mark, its official geometry and its official
// gradients - the icon, not the "GET IT ON" badge. The badge is the piece
// Google's brand guidelines forbid altering, and fitting one into a card
// meant altering it; the icon carries no such condition and, with the store's
// name set under it by the tile itself, says the same thing.
const PLAY_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512"><linearGradient id="kl-play-a" x1="60.6" x2="276.6" y1="45.4" y2="261.4" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#00a0ff"/><stop offset=".01" stop-color="#00a1ff"/><stop offset=".26" stop-color="#00beff"/><stop offset=".51" stop-color="#00d2ff"/><stop offset=".76" stop-color="#00dfff"/><stop offset="1" stop-color="#00e3ff"/></linearGradient><linearGradient id="kl-play-b" x1="446.6" x2="34.3" y1="256" y2="256" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#ffe000"/><stop offset=".41" stop-color="#ffbd00"/><stop offset=".78" stop-color="#ffa500"/><stop offset="1" stop-color="#ff9c00"/></linearGradient><linearGradient id="kl-play-c" x1="349.6" x2="6.9" y1="295.1" y2="637.8" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#ff3a44"/><stop offset="1" stop-color="#c31162"/></linearGradient><linearGradient id="kl-play-d" x1="22.9" x2="176" y1="-38.1" y2="115" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#32a071"/><stop offset=".07" stop-color="#2da771"/><stop offset=".48" stop-color="#15cf74"/><stop offset=".8" stop-color="#06e775"/><stop offset="1" stop-color="#00f076"/></linearGradient><path fill="url(#kl-play-a)" d="M39.6 24.1c-5.6 5.9-8.9 15.1-8.9 27v409.8c0 11.9 3.3 21.1 8.9 27l1.4 1.3L270 259.7v-5.4L41 25.4z"/><path fill="url(#kl-play-b)" d="m346.3 336.3-76.3-76.6v-5.4l76.4-76.5 1.7 1L438.5 231c25.8 14.7 25.8 38.7 0 53.4l-90.4 51.4z"/><path fill="url(#kl-play-c)" d="M348 335.3 270 257 39.6 487.9c8.5 9 22.5 10.1 38.4 1.1z"/><path fill="url(#kl-play-d)" d="M348 178.7 78 25.1C62.1 16 48.1 17.2 39.6 26.2L270 257z"/></svg>';

// Apple's own mark, in currentColor rather than a fixed hex. That is the one
// exception to "vendor marks keep their colours through the hover" on this
// page, and it is the correct treatment for this particular mark: Apple's own
// guidance is a solid logo, dark on light and light on dark, which is exactly
// what inheriting the tile's text colour produces on both sides of the hover.
const APPLE_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 814 1000" fill="currentColor"><path d="M788.1 340.9c-5.8 4.5-108.2 62.2-108.2 190.5 0 148.4 130.3 200.9 134.2 202.2-.6 3.2-20.7 71.9-68.7 141.9-42.8 61.6-87.5 123.1-155.5 123.1s-85.5-39.5-164-39.5c-76.5 0-103.7 40.8-165.9 40.8s-105.6-57-155.5-127C46.7 790.7 0 663 0 541.8c0-194.4 126.4-297.5 250.8-297.5 66.1 0 121.2 43.4 162.7 43.4 39.5 0 101.1-46 176.3-46 28.5 0 130.9 2.6 198.3 99.2zm-234-181.5c31.1-36.9 53.1-88.1 53.1-139.3 0-7.1-.6-14.3-1.9-20.1-50.6 1.9-110.8 33.7-147.1 75.8-28.5 32.4-55.1 83.6-55.1 135.5 0 7.8 1.3 15.6 1.9 18.1 3.2.6 8.4 1.3 13.6 1.3 45.4 0 102.5-30.4 135.5-71.3z"/></svg>';

const CHROME_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 190 190"><linearGradient id="kl-chrome-d" x1="28.3" x2="80.8" y1="75" y2="44.4" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#a52714" stop-opacity=".6"/><stop offset=".7" stop-color="#a52714" stop-opacity="0"/></linearGradient><linearGradient id="kl-chrome-f" x1="109.9" x2="51.5" y1="164.5" y2="130.3" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#055524" stop-opacity=".4"/><stop offset=".3" stop-color="#055524" stop-opacity="0"/></linearGradient><linearGradient id="kl-chrome-h" x1="121.9" x2="136.6" y1="49.8" y2="114.1" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#ea6100" stop-opacity=".3"/><stop offset=".7" stop-color="#ea6100" stop-opacity="0"/></linearGradient><radialGradient id="kl-chrome-a" cx="91.2" cy="55" r="84.1" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#3e2723" stop-opacity=".2"/><stop offset="1" stop-color="#3e2723" stop-opacity="0"/></radialGradient><radialGradient href="#kl-chrome-a" id="kl-chrome-i" cx="20.9" cy="47.5" r="78"/><radialGradient id="kl-chrome-j" cx="94.8" cy="95.1" r="87.9" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#263238" stop-opacity=".2"/><stop offset="1" stop-color="#263238" stop-opacity="0"/></radialGradient><radialGradient id="kl-chrome-k" cx="33.3" cy="31" r="176.8" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#fff" stop-opacity=".1"/><stop offset="1" stop-color="#fff" stop-opacity="0"/></radialGradient><clipPath id="kl-chrome-b"><circle cx="95" cy="95" r="88"/></clipPath><g clip-path="url(#kl-chrome-b)"><use href="#kl-chrome-c" fill="#db4437"/><use href="#kl-chrome-c" fill="url(#kl-chrome-d)"/><use href="#kl-chrome-e" fill="#0f9d58"/><use href="#kl-chrome-e" fill="url(#kl-chrome-f)"/><use href="#kl-chrome-g" fill="#ffcd40"/><use href="#kl-chrome-g" fill="url(#kl-chrome-h)"/><g fill-opacity=".1"><path fill="#3e2723" d="M61.3 114.7 21 47.4l39 67.8z"/><path fill="#263238" d="m128.8 116.3-.8-.4-37.3 67 38.3-67z"/></g><path id="kl-chrome-e" d="M7 183h83.8l39-39v-29H60.2L7 23.5z"/><path id="kl-chrome-g" d="m95 55 34.6 60L91 183h92V55z"/><path id="kl-chrome-c" d="M21 7v108h39.4L95 55h88V7z"/><path fill="url(#kl-chrome-a)" d="M95 55v21l78.4-21z"/><path fill="url(#kl-chrome-i)" d="m21 47.5 57.2 57.2L60.4 115z"/><path fill="url(#kl-chrome-j)" d="m90.8 183 21-78.3 17.8 10.3z"/><circle cx="95" cy="95" r="40" fill="#f1f1f1"/><circle cx="95" cy="95" r="32" fill="#4285f4"/><circle cx="95" cy="95" r="88" fill="url(#kl-chrome-k)"/><g fill="#3e2723" fill-opacity=".1"><path fill="#fff" d="M129.6 115a40 40 0 0 1-69.2 0L7 24.5 60.4 116a40 40 0 0 0 69.2 0z"/><path d="M96 55h-.5a40 40 0 1 1 0 80h.5c22 0 40-18 40-40s-18-40-40-40m-1 127a88 88 0 0 0 88-87.5v.5A88 88 0 0 1 7 95v-.5A88 88 0 0 0 95 182"/><g fill-opacity=".2"><path fill="#fff" d="M130 116.3a39.3 39.3 0 0 0 3.4-32 38 38 0 0 1-3.8 30.7L92 183l38.2-66.5zM95 8a88 88 0 0 1 88 87.5V95A88 88 0 0 0 7 95v.5A88 88 0 0 1 95 8"/><path d="M95 54c-22 0-40 18-40 40v1c0-22 18-40 40-40h88v-1z"/></g></g></g></svg>';

const EDGE_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="1000 1000 25599.6 25600"><linearGradient id="kl-edge-a" gradientUnits="userSpaceOnUse"/><linearGradient href="#kl-edge-a" id="kl-edge-d" x1="6870" x2="24704" y1="18705" y2="18705"><stop offset="0" stop-color="#0c59a4"/><stop offset="1" stop-color="#114a8b"/></linearGradient><linearGradient href="#kl-edge-a" id="kl-edge-g" x1="16272" x2="5133" y1="10968" y2="23102"><stop offset="0" stop-color="#1b9de2"/><stop offset=".16" stop-color="#1595df"/><stop offset=".67" stop-color="#0680d7"/><stop offset="1" stop-color="#0078d4"/></linearGradient><radialGradient href="#kl-edge-a" id="kl-edge-e" cx="16720" cy="18747" r="9538"><stop offset=".72" stop-opacity="0"/><stop offset=".95" stop-opacity=".53"/><stop offset="1"/></radialGradient><radialGradient href="#kl-edge-a" id="kl-edge-h" cx="7130" cy="19866" r="14324" gradientTransform="matrix(.14843 -.98892 .79688 .1196 -8759 25542)"><stop offset=".76" stop-opacity="0"/><stop offset=".95" stop-opacity=".5"/><stop offset="1"/></radialGradient><radialGradient href="#kl-edge-a" id="kl-edge-j" cx="2523" cy="4680" r="20243" gradientTransform="matrix(-.03715 .99931 -2.12836 -.07913 13579 3530)"><stop offset="0" stop-color="#35c1f1"/><stop offset=".11" stop-color="#34c1ed"/><stop offset=".23" stop-color="#2fc2df"/><stop offset=".31" stop-color="#2bc3d2"/><stop offset=".67" stop-color="#36c752"/></radialGradient><radialGradient href="#kl-edge-a" id="kl-edge-k" cx="24247" cy="7758" r="9734" gradientTransform="matrix(.28109 .95968 -.78353 .22949 24510 -16292)"><stop offset="0" stop-color="#66eb6e"/><stop offset="1" stop-color="#66eb6e" stop-opacity="0"/></radialGradient><path id="kl-edge-b" d="M24105 20053a9345 9345 0 0 1-1053 472 10202 10202 0 0 1-3590 646c-4732 0-8855-3255-8855-7432 0-1175 680-2193 1643-2729-4280 180-5380 4640-5380 7253 0 7387 6810 8137 8276 8137 791 0 1984-230 2704-456l130-44a12834 12834 0 0 0 6660-5282c220-350-168-757-535-565"/><path id="kl-edge-f" d="M11571 25141a7913 7913 0 0 1-2273-2137 8145 8145 0 0 1-1514-4740 8093 8093 0 0 1 3093-6395 8082 8082 0 0 1 1373-859c312-148 846-414 1554-404a3236 3236 0 0 1 2569 1297 3184 3184 0 0 1 636 1866c0-21 2446-7960-8005-7960-4390 0-8004 4166-8004 7820 0 2319 538 4170 1212 5604a12833 12833 0 0 0 7684 6757 12795 12795 0 0 0 3908 610c1414 0 2774-233 4045-656a7575 7575 0 0 1-6278-803"/><path id="kl-edge-i" d="M16231 15886c-80 105-330 250-330 566 0 260 170 512 472 723 1438 1003 4149 868 4156 868a5954 5954 0 0 0 3027-839 6147 6147 0 0 0 1133-850 6180 6180 0 0 0 1910-4437c26-2242-796-3732-1133-4392-2120-4141-6694-6525-11668-6525-7011 0-12703 5635-12798 12620 47-3654 3679-6605 7996-6605 350 0 2346 34 4200 1007 1634 858 2490 1894 3086 2921 618 1067 728 2415 728 2952s-271 1333-780 1990z"/><use href="#kl-edge-b" fill="url(#kl-edge-d)"/><use href="#kl-edge-b" fill="url(#kl-edge-e)" opacity=".35"/><use href="#kl-edge-f" fill="url(#kl-edge-g)"/><use href="#kl-edge-f" fill="url(#kl-edge-h)" opacity=".4"/><use href="#kl-edge-i" fill="url(#kl-edge-j)"/><use href="#kl-edge-i" fill="url(#kl-edge-k)"/></svg>';

const BRAVE_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" id="kl-brave-Layer_1" x="0" y="0" viewBox="37.6 0 436.7 511.9"><linearGradient id="kl-brave-SVGID_1_" x1="206.078" x2="642.662" y1="128.75" y2="128.75" gradientTransform="matrix(1 0 0 -1 -168.37 384.75)" gradientUnits="userSpaceOnUse"><stop offset="0" style="stop-color:#f1562b"/><stop offset=".3" style="stop-color:#f1542b"/><stop offset=".41" style="stop-color:#f04d2a"/><stop offset=".49" style="stop-color:#ef4229"/><stop offset=".5" style="stop-color:#ef4029"/><stop offset=".56" style="stop-color:#e83e28"/><stop offset=".67" style="stop-color:#e13c26"/><stop offset="1" style="stop-color:#df3c26"/></linearGradient><path d="m474.3 165.6-15.8-42.9 11-24.6c1.4-3.2.7-6.9-1.7-9.4l-29.9-30.2c-13.1-13.3-32.6-17.8-50.2-11.7l-8.4 2.9L333.7.4 256.3 0h-.5L178 .6l-45.6 49.8-8.1-2.9c-17.7-6.2-37.4-1.6-50.6 11.8L43.3 90.1c-2 2-2.5 4.9-1.4 7.5l11.4 25.5-15.7 42.8 10.2 38.6 46.3 176.1c5.3 20.3 17.6 38.1 34.7 50.2 0 0 56.2 39.7 111.7 75.7 9 7.2 21.9 7.2 30.9 0 62.3-40.8 111.6-75.8 111.6-75.8 17.1-12.2 29.3-30 34.7-50.2l46.1-176.2z" style="fill:url(#kl-brave-SVGID_1_)"/><path d="M268.8 317.4c-3-1.3-6-2.4-9.1-3.3h-5.5c-3.1.9-6.2 2-9.1 3.3l-13.8 5.7c-4.4 1.8-11.4 5.1-15.6 7.2l-25.4 13.2c-2.5.8-3.9 3.6-3.1 6.1.4 1.3 1.3 2.3 2.5 2.9l22.1 15.6c3.9 2.7 10 7.5 13.6 10.8l6.2 5.3c3.6 3.1 9.4 8.2 13 11.4l5.9 5.2c3.8 3.1 9.2 3.1 13 0l6.2-5.4 13-11.3 6.2-5.5c3.6-3.1 9.7-7.9 13.5-10.8l22.1-15.8c2.4-1.1 3.4-4 2.3-6.4-.6-1.2-1.6-2.1-2.9-2.5l-25.4-12.9c-4.2-2.2-11.3-5.4-15.7-7.2z" fill="#fff"/><path d="m425.2 175.2.7-2.3c0-3.1-.1-6.1-.6-9.2-2.1-5.4-4.9-10.6-8.2-15.4l-14.4-21.1c-2.7-3.9-7.2-10.2-10.2-13.9l-19.2-24.1c-1.8-2.3-3.7-4.6-5.7-6.7h-.4s-3.9.7-8.5 1.6l-29.4 5.7-12.9 2.5c-4.3.2-8.6-.4-12.6-1.8L280.6 83c-4.5-1.5-12-3.6-16.6-4.6q-7.35-.75-14.7 0c-4.6 1.1-12.1 3.2-16.6 4.6l-23.2 7.5c-4 1.4-8.3 2-12.6 1.8L184 90l-29.4-5.6c-4.7-.9-8.5-1.6-8.5-1.6h-.4c-2 2.1-4 4.3-5.7 6.7l-19.2 24.1c-2.9 3.6-7.6 10-10.2 13.9l-14.4 21.1c-2.5 3.6-4.7 7.4-6.8 11.3-1.2 4.3-1.9 8.8-1.9 13.3l.7 2.3c.3 1.5.8 2.9 1.3 4.3 3 3.6 8.1 9.5 11.3 13l50.2 53.4c3.4 4 4.4 9.5 2.5 14.4l-8.2 19.5c-1.9 5.2-2 10.8-.4 16l1.7 4.5c2.7 7.3 7.3 13.8 13.4 18.8l7.9 6.4c4.2 3 9.6 3.7 14.4 1.7l28.1-13.4c5.2-2.6 10-5.8 14.4-9.5l22.5-20.3c3.7-3.3 4-9 .6-12.7l-.2-.2-50.7-34.2c-4-2.8-5.3-8.1-3.1-12.5l19.7-37c2.3-4.6 2.6-9.8 1-14.7-2.4-4.6-6.4-8.1-11.2-10.1l-61.6-23.3c-4.4-1.7-4.2-3.6.5-3.9l36.2-3.6c5.7-.4 11.4.1 16.9 1.5l31.5 8.8c4.6 1.4 7.5 6 6.7 10.8l-12.4 67.6c-.8 3.7-1 7.6-.6 11.4.5 1.6 4.7 3.6 9.4 4.7l19.2 4.1c5.6 1 11.3 1 16.9 0l17.3-3.9c4.6-1 8.8-3.2 9.4-4.8.4-3.8.2-7.7-.6-11.4l-12.5-67.6c-.8-4.8 2.1-9.4 6.7-10.8l31.5-8.8c5.5-1.4 11.2-1.9 16.9-1.5l36.2 3.4c4.7.4 5 2.2.5 3.9l-61.6 23.4c-4.8 2-8.7 5.6-11.2 10.1-1.6 4.8-1.3 10.1 1 14.7l19.7 37c2.2 4.3.9 9.6-3.1 12.5l-50.7 34.2c-3.4 3.6-3.3 9.3.3 12.7l.2.2 22.5 20.3c4.4 3.7 9.2 6.9 14.4 9.5l28.1 13.3c4.8 1.9 10.2 1.3 14.4-1.8l7.9-6.5c6.1-5 10.7-11.4 13.4-18.8l1.7-4.5c1.6-5.2 1.5-10.9-.4-16l-8.3-19.5c-1.8-4.9-.8-10.4 2.5-14.4l50.2-53.5c3.3-3.6 8.3-9.3 11.3-13 .5-1.3 1-2.7 1.4-4.2" fill="#fff"/></svg>';

const OPERA_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512"><g transform="scale(.048)"><linearGradient id="kl-opera-a" x1="112824.305" x2="112824.305" y1="-50348.805" y2="-60686.363" gradientTransform="matrix(.048 0 0 -.048 -971.833 -2242.833)" gradientUnits="userSpaceOnUse"><stop offset=".3" style="stop-color:#ff1b2d"/><stop offset=".438" style="stop-color:#fa1a2c"/><stop offset=".594" style="stop-color:#ed1528"/><stop offset=".758" style="stop-color:#d60e21"/><stop offset=".927" style="stop-color:#b70519"/><stop offset="1" style="stop-color:#a70014"/></linearGradient><path d="M3577.6 8339.2c-589.9-696.5-971.7-1724.8-997.3-2880v-251.7c25.6-1155.2 407.5-2183.5 997.3-2880C4343.5 1334.4 5480.5 704 6749.9 704c780.8 0 1512.5 238.9 2137.6 653.9C7948.8 517.3 6711.5 5.3 5353.6 0h-20.3C2388.3 0 0 2388.3 0 5333.3c0 2860.8 2251.7 5194.7 5079.5 5326.9 84.3 4.3 168.5 6.4 253.9 6.4 1365.3 0 2611.2-513.1 3554.1-1356.8-625.1 413.9-1355.7 652.8-2137.6 652.8-1269.4.1-2406.4-630.3-3172.3-1623.4" style="fill:url(#kl-opera-a)"/><linearGradient id="kl-opera-b" x1="168624.297" x2="168624.297" y1="-63042.582" y2="-72185.563" gradientTransform="matrix(.048 0 0 -.048 -971.833 -2242.833)" gradientUnits="userSpaceOnUse"><stop offset="0" style="stop-color:#9c0000"/><stop offset=".7" style="stop-color:#ff4b4b"/></linearGradient><path d="M3577.6 2327.5c489.6-578.1 1121.1-925.9 1812.3-925.9 1553.1 0 2810.7 1760 2810.7 3931.7S6941.9 9265.1 5389.9 9265.1c-690.1 0-1322.7-348.8-1812.3-925.9 765.9 993.1 1902.9 1623.5 3172.3 1623.5 780.8 0 1512.5-238.9 2137.6-652.8 1092.3-977.1 1779.2-2396.8 1779.2-3976.5s-686.9-2999.5-1779.2-3975.5C8262.4 942.9 7531.7 704 6749.9 704c-1269.4 0-2406.4 630.4-3172.3 1623.5" style="fill:url(#kl-opera-b)"/></g></svg>';

const VIVALDI_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1900 1900"><path fill="#ef3939" d="M944 1830c386 0 600 0 740-140s140-354 140-740 0-600-140-740S1330 70 944 70s-600 0-740 140S64 564 64 950s0 600 140 740 354 140 740 140"/><linearGradient id="kl-vivaldi-a" x1="61.24" x2="145.33" y1="37.94" y2="183.58" gradientUnits="userSpaceOnUse"><stop offset="0" stop-opacity=".2"/><stop offset=".79" stop-opacity=".05"/></linearGradient><path fill="url(#kl-vivaldi-a)" d="M151.6 62.4A66 66 0 0 0 30.5 78a65.57 65.57 0 0 0 6.8 50.4c.1.2.2.4.4.6l31 53.8 25.5.2c17.1 0 30.9 0 42.2-1.2 14-1.5 24.1-5 31.9-12.8 11.3-11.3 13.5-27.6 13.9-53.8z" transform="scale(10)"/><path fill="#fff" d="M1407 484a657.9 657.9 0 0 0-932 0 660.9 660.9 0 0 0 0 933 657.9 657.9 0 0 0 932 0 660.9 660.9 0 0 0 0-933m-39 304-326 567c-20 35-49 56-90 59-45 3-80-16-103-55L519 786c-42-73 5-162 89-166 44-2 78 18 101 57 31 52 61 105 91 158l66 114c33 55 80 85 144 89 90 5 174-60 185-156 1-7 1-14 2-18 0-31-6-57-19-82-34-68 2-143 75-160 60-13 121 31 129 91 4 27-1 52-14 75"/></svg>';

const FIREFOX_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="8.06 -0.07 495.87 512.11"><g transform="translate(.697 .72)scale(.98198)"><linearGradient id="kl-firefox-a" x1="470.559" x2="50.986" y1="438.589" y2="33.772" gradientTransform="matrix(.982 0 0 -.982 -1.496 510.756)" gradientUnits="userSpaceOnUse"><stop offset=".048" style="stop-color:#fff44f"/><stop offset=".111" style="stop-color:#ffe847"/><stop offset=".225" style="stop-color:#ffc830"/><stop offset=".368" style="stop-color:#ff980e"/><stop offset=".401" style="stop-color:#ff8b16"/><stop offset=".462" style="stop-color:#ff672a"/><stop offset=".534" style="stop-color:#ff3647"/><stop offset=".705" style="stop-color:#e31587"/></linearGradient><path d="M494.1 174.3c-11-26.4-33.3-55-50.7-64 12.4 24.1 21.1 50 25.6 76.7v.4c-28.6-71.2-77-100-116.6-162.5-2-3.2-4-6.3-6-9.7-1.1-1.9-2-3.6-2.8-5.2-1.6-3.2-2.9-6.5-3.8-10 0-.3-.2-.6-.6-.7h-.5l-.1.1s-.1.1-.2.1l.1-.2c-63.5 37.2-85 106-87 140.4-25.4 1.7-49.6 11.1-69.6 26.8-2.1-1.8-4.3-3.4-6.5-4.9-5.8-20.2-6-41.5-.7-61.8-23.3 11.3-44 27.3-60.8 47h-.1c-10-12.7-9.3-54.6-8.7-63.3q-4.5 1.8-8.4 4.5c-8.8 6.3-17.1 13.4-24.7 21.2-8.7 8.8-16.6 18.3-23.6 28.3-16.2 23-27.8 49-33.9 76.6l-.3 1.7c-.5 2.2-2.2 13.4-2.5 15.8 0 .2 0 .4-.1.6C9.4 243.7 8 255.3 7.5 267v1.3c.2 139.6 113.6 252.5 253.2 252.3 122.8-.2 227.7-88.6 248.6-209.6.4-3.3.8-6.5 1.1-9.8 5.3-43-.3-86.6-16.3-126.9M202.8 372.2c1.2.6 2.3 1.2 3.5 1.7l.2.1c-1.2-.6-2.4-1.2-3.7-1.8m266.3-184.6v-.2z" style="fill:url(#kl-firefox-a)"/><radialGradient id="kl-firefox-b" cx="-7667.514" cy="9141.38" r="526.888" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset=".129" style="stop-color:#ffbd4f"/><stop offset=".186" style="stop-color:#ffac31"/><stop offset=".247" style="stop-color:#ff9d17"/><stop offset=".283" style="stop-color:#ff980e"/><stop offset=".403" style="stop-color:#ff563b"/><stop offset=".467" style="stop-color:#ff3750"/><stop offset=".71" style="stop-color:#f5156c"/><stop offset=".782" style="stop-color:#eb0878"/><stop offset=".86" style="stop-color:#e50080"/></radialGradient><path d="M494.1 174.3c-11-26.4-33.3-55-50.7-64 12.4 24.1 21.1 50 25.6 76.7v.5c19.5 55.7 16.7 116.9-7.9 170.6-29 62.2-99.1 125.9-208.8 122.7-118.5-3.4-223-91.4-242.5-206.6-3.6-18.2 0-27.4 1.8-42.2-2.4 11.5-3.8 23.1-4.1 34.9v1.3c.2 139.6 113.6 252.5 253.2 252.3 122.9-.1 227.7-88.5 248.7-209.5.4-3.3.8-6.5 1.1-9.8 5.2-43-.4-86.6-16.4-126.9" style="fill:url(#kl-firefox-b)"/><radialGradient id="kl-firefox-c" cx="-7866.73" cy="8922.242" r="526.888" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset=".3" style="stop-color:#960e18"/><stop offset=".351" style="stop-color:#b11927;stop-opacity:.74"/><stop offset=".435" style="stop-color:#db293d;stop-opacity:.343"/><stop offset=".497" style="stop-color:#f5334b;stop-opacity:9.400000e-02"/><stop offset=".53" style="stop-color:#ff3750;stop-opacity:0"/></radialGradient><path d="M494.1 174.3c-11-26.4-33.3-55-50.7-64 12.4 24.1 21.1 50 25.6 76.7v.5c19.5 55.7 16.7 116.9-7.9 170.6-29 62.2-99.1 125.9-208.8 122.7-118.5-3.4-223-91.4-242.5-206.6-3.6-18.2 0-27.4 1.8-42.2-2.4 11.5-3.8 23.1-4.1 34.9v1.3c.2 139.6 113.6 252.5 253.2 252.3 122.9-.1 227.7-88.5 248.7-209.5.4-3.3.8-6.5 1.1-9.8 5.2-43-.4-86.6-16.4-126.9" style="fill:url(#kl-firefox-c)"/><radialGradient id="kl-firefox-d" cx="-7800.325" cy="9260.909" r="381.667" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset=".132" style="stop-color:#fff44f"/><stop offset=".252" style="stop-color:#ffdc3e"/><stop offset=".506" style="stop-color:#ff9d12"/><stop offset=".526" style="stop-color:#ff980e"/></radialGradient><path d="M371.3 204c.5.4 1.1.8 1.6 1.2-6.3-11.3-14.3-21.6-23.5-30.6C270.8 96 328.8 4.2 338.5-.5l.1-.1c-63.5 37.2-85 106-87 140.4 2.9-.2 5.9-.4 8.9-.4 45.9 0 88.2 24.7 110.8 64.6" style="fill:url(#kl-firefox-d)"/><radialGradient id="kl-firefox-e" cx="-7926.495" cy="8782.792" r="250.858" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset=".353" style="stop-color:#3a8ee6"/><stop offset=".472" style="stop-color:#5c79f0"/><stop offset=".669" style="stop-color:#9059ff"/><stop offset="1" style="stop-color:#c139e6"/></radialGradient><path d="M260.7 219.7c-.4 6.3-22.6 28-30.4 28-71.9 0-83.5 43.5-83.5 43.5 3.2 36.6 28.7 66.8 59.5 82.7 1.4.7 2.8 1.4 4.3 2 2.5 1.1 4.9 2.1 7.4 3 10.6 3.7 21.7 5.9 32.9 6.3 126 5.9 150.4-150.6 59.5-196.1 21.4-2.8 43.2 2.5 60.9 14.8-22.6-39.9-64.9-64.6-110.7-64.7-3 0-5.9.2-8.9.4-25.4 1.7-49.6 11.1-69.6 26.8 3.9 3.3 8.2 7.6 17.4 16.6 17.1 17.1 61.1 34.6 61.2 36.7" style="fill:url(#kl-firefox-e)"/><radialGradient id="kl-firefox-f" cx="-7931.817" cy="8971.409" r="133.026" gradientTransform="matrix(.9545 -.2308 -.27 -1.1175 10267.805 8423.169)" gradientUnits="userSpaceOnUse"><stop offset=".206" style="stop-color:#9059ff;stop-opacity:0"/><stop offset=".278" style="stop-color:#8c4ff3;stop-opacity:6.400000e-02"/><stop offset=".747" style="stop-color:#7716a8;stop-opacity:.45"/><stop offset=".975" style="stop-color:#6e008b;stop-opacity:.6"/></radialGradient><path d="M260.7 219.7c-.4 6.3-22.6 28-30.4 28-71.9 0-83.5 43.5-83.5 43.5 3.2 36.6 28.7 66.8 59.5 82.7 1.4.7 2.8 1.4 4.3 2 2.5 1.1 4.9 2.1 7.4 3 10.6 3.7 21.7 5.9 32.9 6.3 126 5.9 150.4-150.6 59.5-196.1 21.4-2.8 43.2 2.5 60.9 14.8-22.6-39.9-64.9-64.6-110.7-64.7-3 0-5.9.2-8.9.4-25.4 1.7-49.6 11.1-69.6 26.8 3.9 3.3 8.2 7.6 17.4 16.6 17.1 17.1 61.1 34.6 61.2 36.7" style="fill:url(#kl-firefox-f)"/><radialGradient id="kl-firefox-g" cx="-7873.37" cy="9161.301" r="180.498" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset="0" style="stop-color:#ffe226"/><stop offset=".121" style="stop-color:#ffdb27"/><stop offset=".295" style="stop-color:#ffc82a"/><stop offset=".502" style="stop-color:#ffa930"/><stop offset=".732" style="stop-color:#ff7e37"/><stop offset=".792" style="stop-color:#ff7139"/></radialGradient><path d="M170.3 158.2c2 1.3 3.7 2.4 5.2 3.5-5.8-20.2-6-41.5-.7-61.8-23.3 11.3-44 27.3-60.8 47 1.2 0 37.9-.7 56.3 11.3" style="fill:url(#kl-firefox-g)"/><radialGradient id="kl-firefox-h" cx="-7727.279" cy="9280.831" r="770.116" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset=".113" style="stop-color:#fff44f"/><stop offset=".456" style="stop-color:#ff980e"/><stop offset=".622" style="stop-color:#ff5634"/><stop offset=".716" style="stop-color:#ff3647"/><stop offset=".904" style="stop-color:#e31587"/></radialGradient><path d="M9.8 274.3c19.5 115.2 124 203.3 242.5 206.6C362 484 432.1 420.3 461.1 358.2c24.5-53.7 27.3-114.8 7.9-170.6v-.4.4c9 58.5-20.8 115.2-67.4 153.6l-.1.3c-90.7 73.9-177.5 44.6-195 32.6-1.2-.6-2.5-1.2-3.7-1.8-52.9-25.3-74.7-73.4-70-114.8-25.6.4-49.1-14.4-59.9-37.7 28.2-17.3 63.4-18.7 92.9-3.7 29.9 13.6 64 14.9 94.9 3.7-.1-2.1-44.1-19.6-61.2-36.5-9.2-9-13.5-13.4-17.4-16.6-2.1-1.8-4.3-3.4-6.5-4.9-1.5-1-3.2-2.1-5.2-3.5-18.4-12-55.1-11.3-56.3-11.3h-.1c-10-12.7-9.3-54.6-8.7-63.3q-4.5 1.8-8.4 4.5c-8.8 6.3-17.1 13.4-24.7 21.2-8.7 8.7-16.6 18.2-23.7 28.3-16.2 23-27.8 49-33.9 76.6-.3.4-9.2 39.7-4.8 60" style="fill:url(#kl-firefox-h)"/><radialGradient id="kl-firefox-i" cx="-7976.017" cy="9823.985" r="564.057" gradientTransform="matrix(.1031 .9771 .6412 -.06776 -5155.366 8422.637)" gradientUnits="userSpaceOnUse"><stop offset="0" style="stop-color:#fff44f"/><stop offset=".06" style="stop-color:#ffe847"/><stop offset=".168" style="stop-color:#ffc830"/><stop offset=".304" style="stop-color:#ff980e"/><stop offset=".356" style="stop-color:#ff8b16"/><stop offset=".455" style="stop-color:#ff672a"/><stop offset=".57" style="stop-color:#ff3647"/><stop offset=".737" style="stop-color:#e31587"/></radialGradient><path d="M349.4 174.5c9.2 9.1 17.1 19.4 23.5 30.6 1.4 1 2.7 2.1 3.8 3.1C434 261 404 335.7 401.7 341c46.5-38.3 76.3-95 67.3-153.6-28.6-71.3-77.1-100-116.6-162.6-2-3.2-4-6.3-6-9.7-1.1-1.9-2-3.6-2.8-5.2-1.6-3.2-2.9-6.5-3.8-10 0-.3-.2-.6-.6-.7h-.5l-.1.1s-.1.1-.2.1c-9.8 4.6-67.8 96.4 10.8 175z" style="fill:url(#kl-firefox-i)"/><radialGradient id="kl-firefox-j" cx="-7873.37" cy="9094.897" r="480.72" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset=".137" style="stop-color:#fff44f"/><stop offset=".48" style="stop-color:#ff980e"/><stop offset=".592" style="stop-color:#ff5634"/><stop offset=".655" style="stop-color:#ff3647"/><stop offset=".904" style="stop-color:#e31587"/></radialGradient><path d="M376.6 208.3c-1.1-1-2.4-2.1-3.8-3.1-.5-.4-1-.8-1.6-1.2-17.8-12.3-39.5-17.6-60.9-14.8 90.9 45.5 66.5 202-59.5 196.1-11.2-.5-22.3-2.6-32.9-6.3-2.5-.9-4.9-1.9-7.4-3-1.4-.7-2.9-1.3-4.3-2l.2.1c17.6 12 104.3 41.3 195-32.6l.1-.3c2.4-5.4 32.4-80-24.9-132.9" style="fill:url(#kl-firefox-j)"/><radialGradient id="kl-firefox-k" cx="-7747.201" cy="9068.334" r="526.17" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset=".094" style="stop-color:#fff44f"/><stop offset=".231" style="stop-color:#ffe141"/><stop offset=".509" style="stop-color:#ffaf1e"/><stop offset=".626" style="stop-color:#ff980e"/></radialGradient><path d="M146.7 291.1s11.7-43.5 83.5-43.5c7.8 0 30-21.7 30.4-28-30.9 11.2-65 9.9-94.9-3.7-29.5-15-64.7-13.6-92.9 3.7 10.8 23.3 34.2 38 59.9 37.7-4.7 41.3 17.2 89.5 70 114.8 1.2.6 2.3 1.2 3.5 1.7-30.8-15.9-56.3-46.1-59.5-82.7" style="fill:url(#kl-firefox-k)"/><linearGradient id="kl-firefox-l" x1="465.416" x2="108.463" y1="440.741" y2="83.722" gradientTransform="matrix(.982 0 0 -.982 -1.496 510.756)" gradientUnits="userSpaceOnUse"><stop offset=".167" style="stop-color:#fff44f;stop-opacity:.8"/><stop offset=".266" style="stop-color:#fff44f;stop-opacity:.634"/><stop offset=".489" style="stop-color:#fff44f;stop-opacity:.217"/><stop offset=".6" style="stop-color:#fff44f;stop-opacity:0"/></linearGradient><path d="M494.1 174.3c-11-26.4-33.3-55-50.7-64 12.4 24.1 21.1 50 25.6 76.7v.4c-28.6-71.2-77-100-116.6-162.5-2-3.2-4-6.3-6-9.7-1.1-1.9-2-3.6-2.8-5.2-1.6-3.2-2.9-6.5-3.8-10 0-.3-.2-.6-.6-.7h-.5l-.1.1s-.1.1-.2.1l.1-.2c-63.5 37.2-85 106-87 140.4 2.9-.2 5.9-.4 8.9-.4 45.8.1 88.1 24.8 110.7 64.7-17.8-12.3-39.5-17.6-60.9-14.8 90.9 45.5 66.5 202-59.5 196.1-11.2-.5-22.3-2.6-32.9-6.3-2.5-.9-4.9-1.9-7.4-3-1.4-.7-2.9-1.3-4.3-2l.2.1c-1.2-.6-2.5-1.2-3.7-1.8 1.2.6 2.3 1.2 3.5 1.7-30.9-16-56.3-46.1-59.5-82.7 0 0 11.7-43.5 83.5-43.5 7.8 0 30-21.7 30.4-28-.1-2.1-44.1-19.6-61.2-36.5-9.2-9-13.5-13.4-17.4-16.6-2.1-1.8-4.3-3.4-6.5-4.9-5.8-20.2-6-41.5-.7-61.8-23.3 11.3-44 27.3-60.8 47h.1c-10-12.7-9.3-54.6-8.7-63.3q-4.5 1.8-8.4 4.5c-8.8 6.3-17.1 13.4-24.7 21.2-8.7 8.8-16.6 18.3-23.6 28.3-16.2 23-27.8 49-33.9 76.6l-.3 1.7c-.5 2.2-2.6 13.5-2.9 15.9-2 11.7-3.2 23.4-3.7 35.2v1.3C8 408 121.4 520.9 261 520.7c122.8-.2 227.6-88.6 248.6-209.6.4-3.3.8-6.5 1.1-9.8 5-43-.6-86.7-16.6-127m-25.1 13v.3z" style="fill:url(#kl-firefox-l)"/></g></svg>';

function LogoChrome() {
  return <BrandMark svg={CHROME_SVG} />;
}

function LogoEdge() {
  return <BrandMark svg={EDGE_SVG} />;
}

function LogoBrave() {
  return <BrandMark svg={BRAVE_SVG} />;
}

function LogoOpera() {
  return <BrandMark svg={OPERA_SVG} />;
}

function LogoVivaldi() {
  return <BrandMark svg={VIVALDI_SVG} />;
}

function LogoFirefox() {
  return <BrandMark svg={FIREFOX_SVG} />;
}

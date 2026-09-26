import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import logoUrl from '../../assets/logo.svg';
import { buildBookmarklet } from '../../lib/browserTools';
import { appAddress, withBase } from '../../lib/basePath';
import { ARCH_LABEL, desktopSlug, visitorArch, type Arch, type DesktopOS, type DesktopSlug } from '../../lib/desktopBuild';
import { fetchDeploymentInfo, fetchExtensionVersion, fetchHealth } from '../../lib/api';
import { copyToClipboard } from '../../lib/clipboard';
import { useInstallPrompt } from '../../lib/pwaInstall';
import { followExternal, openExternal } from '../../lib/external';
import { useT } from '../../lib/i18n';
import { qrMatrix } from '../../lib/qrmatrix';
import { openWindow } from '../../lib/windowStack';
import { ANDROID_SVG, APPLE_SVG, DOCKER_SVG, LINUX_SVG, PLAY_SVG, UNRAID_SVG, WINDOWS_SVG, ZIP_SVG } from '../../lib/appMarks';
import { BrandMark, ReadmeButton, type ReadmePart } from '../../components/ReadmeButton';
import { QRCode } from '../../components/QRCode';
import { Button, Card, InfoBubble, SectionTitle } from '../../components/ui';
import { releaseTag } from './Help';

/**
 * BrowserTools is the App page (GlimStone's "The App tab"): every way to get
 * KnightLoader that the reader is not already using, as the download buttons
 * of the README. The phone card stands everywhere, and the second card offers
 * the forms this build is not: the desktop app in a container, a server in the
 * desktop app. Below them come the bookmarklet and the browser extension,
 * which send links in from any page. The extension download is byte-identical
 * to the source, since it is set up with the connection phrase and holds no
 * address. The bookmarklet and the PWA share target land on /quickadd.
 */
export function BrowserTools() {
  const { t } = useT();
  const bookmarklet = buildBookmarklet(appAddress());
  const [copied, setCopied] = useState(false);
  const [copies, setCopies] = useState(0);
  const [extensionVersion, setExtensionVersion] = useState<string | null>(null);
  const [deployment, setDeployment] = useState<string | null>(null);
  useEffect(() => {
    void fetchExtensionVersion()
      .then((v) => setExtensionVersion(v.version))
      .catch(() => {});
    void fetchDeploymentInfo()
      .then((d) => setDeployment(d.deployment))
      .catch(() => {});
  }, []);

  const chromiumHint = (
    <ol className="list-decimal space-y-1 ps-4">
      <li>{t('settings.browsertools.installChromiumStep1')}</li>
      <li>{t('settings.browsertools.installChromiumStep2')}</li>
      <li>{t('settings.browsertools.installChromiumStep3')}</li>
      <li>{t('settings.browsertools.installChromiumStep4')}</li>
      {/* Chrome keeps a freshly loaded extension off the toolbar until it is
          pinned, which otherwise reads as a failed install. */}
      <li>{t('settings.browsertools.installChromiumStep5')}</li>
    </ol>
  );
  const firefoxHint = (
    <ol className="list-decimal space-y-1 ps-4">
      <li>{t('settings.browsertools.installFirefoxStep1')}</li>
      <li>{t('settings.browsertools.installFirefoxStep2')}</li>
      <li>{t('settings.browsertools.installFirefoxStep3')}</li>
    </ol>
  );
  const installLabel = t('settings.browsertools.installLabel');
  const soon = t('settings.browsertools.soon');

  return (
    <div className="flex flex-col gap-10">
      <PhoneCard />
      {/* Nothing until the build is known, so the card for the other form
          never flashes up first. */}
      {deployment === 'desktop' ? <ServerCard /> : deployment !== null && <DesktopCard />}

      <Card hue={2} className="flex flex-col gap-3">
        <SectionTitle
          hint={
            <ol className="list-decimal space-y-1 ps-4">
              <li>{t('settings.browsertools.bookmarkletStep1')}</li>
              <li>{t('settings.browsertools.bookmarkletStep2')}</li>
            </ol>
          }
        >
          {t('settings.browsertools.bookmarkletTitle')}
        </SectionTitle>
        <div className="flex flex-wrap items-center gap-3">
          {/* A real javascript: link, since only that can be dragged into a
              bookmarks bar. React 19 swaps a javascript: href for one that
              throws, so the href is set on the node, which React leaves
              alone. */}
          <a
            ref={(a) => a?.setAttribute('href', bookmarklet)}
            onClick={(e) => e.preventDefault()}
            className="inline-flex items-center gap-2 rounded-[var(--radius-pill)] bg-carbon-surface2
              px-3.5 py-2 text-sm font-medium text-carbon-text hover:bg-carbon-surface3"
          >
            <img src={logoUrl} alt="" aria-hidden className="h-5 w-auto shrink-0" />
            {t('settings.browsertools.bookmarkletLink')}
          </a>
          {'clipboard' in navigator && (
            <Button
              kind="ghost"
              className="px-2.5 text-xs"
              confirm={copies}
              onClick={async () => {
                await navigator.clipboard.writeText(bookmarklet);
                setCopied(true);
                setCopies((n) => n + 1);
                setTimeout(() => setCopied(false), 1800);
              }}
            >
              {copied ? t('settings.browsertools.copied') : t('settings.browsertools.copyCode')}
            </Button>
          )}
        </div>
      </Card>

      <Card hue={3} className="flex flex-col gap-4">
        <SectionTitle
          right={extensionVersion && <ReleaseVersion version={extensionVersion} tagPrefix="extension/v" />}
        >
          {t('settings.browsertools.extensionTitle')}
        </SectionTitle>
        {/* The README's two extension buttons: one package for every Chromium
            browser, which the second line names, and the Firefox add-on. Each
            (i) holds the steps that install it. */}
        <div className="glim-readme-btn-rows">
          <ReadmeButton
            brand="chrome"
            parts={[{ name: 'Chrome', sub: 'Edge, Brave', onClick: storeOr('Chrome', downloadZip) }]}
            mark={<BrandMark svg={CHROME_SVG} lit={CHROME_LIT_SVG} />}
            hint={chromiumHint}
            hintLabel={installLabel}
            soonLabel={soon}
          />
          <ReadmeButton
            brand="firefox"
            parts={[
              { name: 'Firefox', sub: t('settings.browsertools.firefoxSub'), onClick: storeOr('Firefox', downloadXpi) },
            ]}
            mark={<BrandMark svg={FIREFOX_SVG} lit={FIREFOX_LIT_SVG} />}
            hint={firefoxHint}
            hintLabel={installLabel}
            soonLabel={soon}
          />
        </div>
      </Card>
    </div>
  );
}

/**
 * PhoneCard offers the Android app, and installing this page as a web app as
 * the smaller second option. There is no iPhone build, so there is no App
 * Store button.
 */
function PhoneCard() {
  const { t } = useT();
  const { available: canInstall, promptInstall } = useInstallPrompt();
  // Safari never fires beforeinstallprompt, so iOS gets the Share-sheet steps.
  const iOS = /iphone|ipad|ipod/i.test(navigator.userAgent);
  const soon = t('settings.browsertools.soon');

  return (
    <Card hue={0} className="flex flex-col gap-4">
      <SectionTitle
        hint={t('settings.browsertools.phoneHint')}
        right={<ReleaseVersion version={APP_VERSION} tagPrefix="mobile/v" />}
      >
        {t('settings.browsertools.phoneTitle')}
      </SectionTitle>
      <div className="glim-readme-btn-rows">
        <ReadmeButton
          brand="play"
          parts={[{ name: t('settings.browsertools.storeAndroid'), href: APP_URLS.play }]}
          mark={<BrandMark svg={PLAY_SVG} />}
          soonLabel={soon}
        />
        <ApkButton />
      </div>
      {(canInstall || iOS) && (
        <div className="flex flex-col gap-2 pt-1">
          {/* The iOS steps sit in a bubble on the caption, shown only where the
              browser offers no Install button. */}
          <span className="flex items-center gap-1.5 text-xs font-semibold text-carbon-textSub">
            {t('settings.browsertools.installPwaLabel')}
            {!canInstall && iOS && <InfoBubble tip={t('settings.browsertools.installIOS')} />}
          </span>
          {canInstall && (
            <div>
              <Button kind="secondary" hue={0} onClick={() => void promptInstall()}>
                {t('settings.browsertools.install')}
              </Button>
            </div>
          )}
        </div>
      )}
    </Card>
  );
}

/**
 * ApkButton is the APK with its QR code as a segment. The page is usually open
 * on a computer and the file is wanted on the phone, but a code cannot be
 * scanned inside a button this height, so the segment opens it in a window
 * over the button.
 */
function ApkButton() {
  const { t } = useT();
  const [qr, setQr] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  const close = useCallback(() => setQr(false), []);
  const matrix = useMemo(() => qrMatrix(APP_URLS.apk), []);
  const label = t('settings.browsertools.qrCode');
  return (
    <div ref={box} className="inline-flex">
      <ReadmeButton
        brand="android"
        parts={[
          { name: 'Android', sub: 'APK', href: APP_URLS.apk },
          { name: label, sub: 'APK', onClick: () => setQr((open) => !open) },
        ]}
        mark={<BrandMark svg={ANDROID_SVG} />}
        markClass="glim-android-mark"
      />
      {qr && (
        <QrWindow anchor={box} label={label} onClose={close}>
          <QRCode matrix={matrix} label={APP_URLS.apk} size={168} />
        </QrWindow>
      )}
    </div>
  );
}

/**
 * QrWindow holds the code in a small window of its own, drawn into the body
 * because the button's unit clips everything inside its edge. It stands above
 * the button where there is room and below it where there is not. Escape, a
 * press elsewhere, a scroll and a resize close it, since the last two carry
 * the button away from it.
 */
function QrWindow({
  anchor,
  label,
  onClose,
  children,
}: {
  anchor: RefObject<HTMLDivElement | null>;
  label: string;
  onClose: () => void;
  children: ReactNode;
}) {
  const win = useRef<HTMLDivElement>(null);
  const [at, setAt] = useState<{ left: number; top: number } | null>(null);

  useLayoutEffect(() => {
    const box = anchor.current?.getBoundingClientRect();
    const own = win.current;
    if (!box || !own) return;
    const margin = 8;
    const { offsetWidth: width, offsetHeight: height } = own;
    const above = box.top - margin - height >= margin;
    const left = Math.max(margin, Math.min(window.innerWidth - margin - width, box.right - width));
    setAt({ left, top: above ? box.top - margin - height : box.bottom + margin });
  }, [anchor]);

  useEffect(() => {
    const own = win.current;
    if (!own) return;
    const leave = openWindow(own, onClose);
    // A press on the segment is left to its own click, which closes the window.
    const onDown = (e: PointerEvent) => {
      const target = e.target as Node;
      if (own.contains(target) || anchor.current?.contains(target)) return;
      onClose();
    };
    document.addEventListener('pointerdown', onDown);
    window.addEventListener('scroll', onClose, true);
    window.addEventListener('resize', onClose);
    return () => {
      leave();
      document.removeEventListener('pointerdown', onDown);
      window.removeEventListener('scroll', onClose, true);
      window.removeEventListener('resize', onClose);
    };
  }, [anchor, onClose]);

  return createPortal(
    <div
      ref={win}
      role="dialog"
      aria-label={label}
      className="glim-fade fixed z-50 rounded-[var(--radius-card)] shadow-[var(--elevation)]"
      style={{ left: at?.left ?? 0, top: at?.top ?? 0, visibility: at ? 'visible' : 'hidden' }}
    >
      {children}
    </div>,
    document.body,
  );
}

/**
 * DesktopCard offers the desktop app to a reader in a container or a browser.
 * The Windows and Linux buttons give the build for this computer's
 * architecture, x64 where the browser does not tell, and the segment beside
 * each the other one.
 */
function DesktopCard() {
  const { t } = useT();
  const [arch, setArch] = useState<Arch>('x64');
  useEffect(() => {
    void visitorArch().then((found) => found && setArch(found));
  }, []);
  const other: Arch = arch === 'x64' ? 'arm64' : 'x64';
  // The build for this computer first, the other architecture as its segment.
  const parts = (os: DesktopOS, name: string): [ReadmePart, ReadmePart] => [
    { name, sub: ARCH_LABEL[arch], href: desktopZip(desktopSlug(os, arch)) },
    { name: ARCH_LABEL[other], sub: name, href: desktopZip(desktopSlug(os, other)) },
  ];

  return (
    <Card hue={1} className="flex flex-col gap-4">
      <SectionTitle
        hint={
          <>
            <span className="block">{t('settings.browsertools.desktopHint')}</span>
            <span className="mt-1.5 block">{t('settings.browsertools.desktopArchHint')}</span>
          </>
        }
      >
        {t('settings.browsertools.desktopTitle')}
      </SectionTitle>
      <div className="glim-readme-btn-rows">
        <ReadmeButton
          brand="windows"
          parts={parts('windows', 'Windows')}
          mark={<BrandMark svg={WINDOWS_SVG} />}
          markClass="glim-windows-mark"
        />
        <ReadmeButton
          brand="apple"
          parts={[{ name: 'macOS', sub: 'Universal', href: desktopZip('macos-universal') }]}
          mark={<BrandMark svg={APPLE_SVG} />}
        />
        <ReadmeButton brand="linux" parts={parts('linux', 'Linux')} mark={<BrandMark svg={LINUX_SVG} />} />
      </div>
    </Card>
  );
}

/**
 * ServerCard offers the desktop app's reader a server install: Unraid's
 * Community Applications, the container image and the source code.
 */
function ServerCard() {
  const { t } = useT();
  const soon = t('settings.browsertools.soon');
  const [version, setVersion] = useState('');
  const [copied, setCopied] = useState(false);
  const [copies, setCopies] = useState(0);
  useEffect(() => {
    void fetchHealth()
      .then((h) => setVersion(h.version))
      .catch(() => {});
  }, []);
  useEffect(() => {
    if (!copied) return;
    const id = setTimeout(() => setCopied(false), 1800);
    return () => clearTimeout(id);
  }, [copied]);

  const command = dockerRun();
  // The source of the running version where it is a release, the newest
  // otherwise.
  const tag = releaseTag(version);
  const zip = /^v\d+\.\d+\.\d+$/.test(tag)
    ? `${REPO_URL}/archive/refs/tags/${tag}.zip`
    : `${REPO_URL}/archive/refs/heads/main.zip`;

  return (
    <Card hue={1} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.browsertools.serverHint')}>{t('settings.browsertools.serverTitle')}</SectionTitle>
      <div className="glim-readme-btn-rows">
        <ReadmeButton
          brand="unraid"
          parts={[{ name: 'Unraid', sub: t('settings.browsertools.unraidSub'), href: UNRAID_CA_URL }]}
          mark={<BrandMark svg={UNRAID_SVG} />}
          soonLabel={soon}
        />
        {/* The second line says what a click does, and says "Copied" for a
            moment after one. */}
        <ReadmeButton
          brand="docker"
          parts={[
            {
              name: 'Docker',
              sub: copied ? t('common.copied') : t('settings.browsertools.dockerSub'),
              onClick: () =>
                void copyToClipboard(command).then((ok) => {
                  if (!ok) return;
                  setCopied(true);
                  setCopies((n) => n + 1);
                }),
            },
          ]}
          mark={<BrandMark svg={DOCKER_SVG} />}
          markClass="glim-docker-mark"
          note={copied}
          confirm={copies}
          hint={
            <>
              <span className="block">{t('settings.browsertools.dockerHint')}</span>
              <code dir="ltr" className="mt-1.5 block font-mono text-[11px] [overflow-wrap:anywhere]">
                {command}
              </code>
            </>
          }
          hintLabel={t('settings.browsertools.dockerHint')}
        />
        <ReadmeButton
          brand="zip"
          parts={[{ name: t('settings.browsertools.sourceCode'), sub: t('settings.browsertools.zipArchive'), href: zip }]}
          mark={<BrandMark svg={ZIP_SVG} />}
        />
      </div>
    </Card>
  );
}

const REPO_URL = 'https://github.com/junkerderprovinz/knightloader';

// The app this build offers is the version mobile/app.json names when the page
// is built (vite.config.ts), and the APK tile fetches exactly that release's
// file, so the card's number is the app the tile gives. The standing "newest"
// download is whatever was tagged last, which this build cannot know.
// check-version-sources.mjs holds both ends. An empty listing URL turns its
// tile into one that is still to come.
const APP_VERSION = __MOBILE_VERSION__;

const APP_URLS = {
  play: '',
  apk: `${REPO_URL}/releases/download/mobile/v${APP_VERSION}/KnightLoader-${APP_VERSION}.apk`,
};

const UNRAID_CA_URL = '';

/**
 * desktopZip is the desktop bundle release.yml attaches to every release,
 * under the name without a version that /releases/latest/download/ can find.
 */
function desktopZip(slug: DesktopSlug): string {
  return `${REPO_URL}/releases/latest/download/knightloader-${slug}.zip`;
}

/** The README's docker run on one line, in the reader's own time zone. */
function dockerRun(): string {
  const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  return (
    'docker run -d --name knightloader --restart unless-stopped -p 8749:8749 ' +
    '-v /path/to/appdata:/data -v /path/to/downloads:/data/downloads -v /path/to/watch:/watch ' +
    `-e TZ=${zone} ghcr.io/junkerderprovinz/knightloader:latest`
  );
}

/**
 * ReleaseVersion links a version to its release page. The extension's and the
 * app's tags carry a prefix of their own, since three products share the
 * releases list; a stamp that is not a plain three-part version shows without
 * a link.
 */
function ReleaseVersion({ version, tagPrefix }: { version: string; tagPrefix: 'extension/v' | 'mobile/v' }) {
  const plain = <span className="glim-num text-[11px] text-carbon-textMuted">v{version}</span>;
  if (!/^\d+\.\d+\.\d+$/.test(version)) return plain;
  return (
    <a
      href={`${REPO_URL}/releases/tag/${tagPrefix}${version}`}
      target="_blank"
      rel="noreferrer noopener"
      onClick={followExternal}
      className="glim-num text-[11px] text-carbon-textMuted no-underline hover:text-carbon-text"
    >
      v{version}
    </a>
  );
}

/**
 * Store listings, empty until each goes live. Edge, Brave, Opera and Vivaldi
 * install from the Chrome Web Store; until a URL is set the button downloads
 * the packaged extension instead.
 */
const STORE_URLS: Record<'Chrome' | 'Firefox', string> = {
  Chrome: '',
  Firefox: '',
};

/** storeOr opens the store listing once one exists, else downloads the package. */
function storeOr(store: 'Chrome' | 'Firefox', fallback: () => void): () => void {
  const url = STORE_URLS[store];
  return url ? () => openExternal(url) : fallback;
}

function downloadZip() {
  window.location.href = withBase('/api/browser-extension.zip');
}

function downloadXpi() {
  window.location.href = withBase('/api/browser-extension.xpi');
}

// The browsers' marks carry kl-<name>- ids, apart from the glim- ids of the
// marks in lib/appMarks.ts.
const CHROME_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 190 190"><linearGradient id="kl-chrome-d" x1="28.3" x2="80.8" y1="75" y2="44.4" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#a52714" stop-opacity=".6"/><stop offset=".7" stop-color="#a52714" stop-opacity="0"/></linearGradient><linearGradient id="kl-chrome-f" x1="109.9" x2="51.5" y1="164.5" y2="130.3" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#055524" stop-opacity=".4"/><stop offset=".3" stop-color="#055524" stop-opacity="0"/></linearGradient><linearGradient id="kl-chrome-h" x1="121.9" x2="136.6" y1="49.8" y2="114.1" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#ea6100" stop-opacity=".3"/><stop offset=".7" stop-color="#ea6100" stop-opacity="0"/></linearGradient><radialGradient id="kl-chrome-a" cx="91.2" cy="55" r="84.1" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#3e2723" stop-opacity=".2"/><stop offset="1" stop-color="#3e2723" stop-opacity="0"/></radialGradient><radialGradient href="#kl-chrome-a" id="kl-chrome-i" cx="20.9" cy="47.5" r="78"/><radialGradient id="kl-chrome-j" cx="94.8" cy="95.1" r="87.9" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#263238" stop-opacity=".2"/><stop offset="1" stop-color="#263238" stop-opacity="0"/></radialGradient><radialGradient id="kl-chrome-k" cx="33.3" cy="31" r="176.8" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#fff" stop-opacity=".1"/><stop offset="1" stop-color="#fff" stop-opacity="0"/></radialGradient><clipPath id="kl-chrome-b"><circle cx="95" cy="95" r="88"/></clipPath><g clip-path="url(#kl-chrome-b)"><use href="#kl-chrome-c" fill="#db4437"/><use href="#kl-chrome-c" fill="url(#kl-chrome-d)"/><use href="#kl-chrome-e" fill="#0f9d58"/><use href="#kl-chrome-e" fill="url(#kl-chrome-f)"/><use href="#kl-chrome-g" fill="#ffcd40"/><use href="#kl-chrome-g" fill="url(#kl-chrome-h)"/><g fill-opacity=".1"><path fill="#3e2723" d="M61.3 114.7 21 47.4l39 67.8z"/><path fill="#263238" d="m128.8 116.3-.8-.4-37.3 67 38.3-67z"/></g><path id="kl-chrome-e" d="M7 183h83.8l39-39v-29H60.2L7 23.5z"/><path id="kl-chrome-g" d="m95 55 34.6 60L91 183h92V55z"/><path id="kl-chrome-c" d="M21 7v108h39.4L95 55h88V7z"/><path fill="url(#kl-chrome-a)" d="M95 55v21l78.4-21z"/><path fill="url(#kl-chrome-i)" d="m21 47.5 57.2 57.2L60.4 115z"/><path fill="url(#kl-chrome-j)" d="m90.8 183 21-78.3 17.8 10.3z"/><circle cx="95" cy="95" r="40" fill="#f1f1f1"/><circle cx="95" cy="95" r="32" fill="#4285f4"/><circle cx="95" cy="95" r="88" fill="url(#kl-chrome-k)"/><g fill="#3e2723" fill-opacity=".1"><path fill="#fff" d="M129.6 115a40 40 0 0 1-69.2 0L7 24.5 60.4 116a40 40 0 0 0 69.2 0z"/><path d="M96 55h-.5a40 40 0 1 1 0 80h.5c22 0 40-18 40-40s-18-40-40-40m-1 127a88 88 0 0 0 88-87.5v.5A88 88 0 0 1 7 95v-.5A88 88 0 0 0 95 182"/><g fill-opacity=".2"><path fill="#fff" d="M130 116.3a39.3 39.3 0 0 0 3.4-32 38 38 0 0 1-3.8 30.7L92 183l38.2-66.5zM95 8a88 88 0 0 1 88 87.5V95A88 88 0 0 0 7 95v.5A88 88 0 0 1 95 8"/><path d="M95 54c-22 0-40 18-40 40v1c0-22 18-40 40-40h88v-1z"/></g></g></g></svg>';

const FIREFOX_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="8.06 -0.07 495.87 512.11"><g transform="translate(.697 .72)scale(.98198)"><linearGradient id="kl-firefox-a" x1="470.559" x2="50.986" y1="438.589" y2="33.772" gradientTransform="matrix(.982 0 0 -.982 -1.496 510.756)" gradientUnits="userSpaceOnUse"><stop offset=".048" style="stop-color:#fff44f"/><stop offset=".111" style="stop-color:#ffe847"/><stop offset=".225" style="stop-color:#ffc830"/><stop offset=".368" style="stop-color:#ff980e"/><stop offset=".401" style="stop-color:#ff8b16"/><stop offset=".462" style="stop-color:#ff672a"/><stop offset=".534" style="stop-color:#ff3647"/><stop offset=".705" style="stop-color:#e31587"/></linearGradient><path d="M494.1 174.3c-11-26.4-33.3-55-50.7-64 12.4 24.1 21.1 50 25.6 76.7v.4c-28.6-71.2-77-100-116.6-162.5-2-3.2-4-6.3-6-9.7-1.1-1.9-2-3.6-2.8-5.2-1.6-3.2-2.9-6.5-3.8-10 0-.3-.2-.6-.6-.7h-.5l-.1.1s-.1.1-.2.1l.1-.2c-63.5 37.2-85 106-87 140.4-25.4 1.7-49.6 11.1-69.6 26.8-2.1-1.8-4.3-3.4-6.5-4.9-5.8-20.2-6-41.5-.7-61.8-23.3 11.3-44 27.3-60.8 47h-.1c-10-12.7-9.3-54.6-8.7-63.3q-4.5 1.8-8.4 4.5c-8.8 6.3-17.1 13.4-24.7 21.2-8.7 8.8-16.6 18.3-23.6 28.3-16.2 23-27.8 49-33.9 76.6l-.3 1.7c-.5 2.2-2.2 13.4-2.5 15.8 0 .2 0 .4-.1.6C9.4 243.7 8 255.3 7.5 267v1.3c.2 139.6 113.6 252.5 253.2 252.3 122.8-.2 227.7-88.6 248.6-209.6.4-3.3.8-6.5 1.1-9.8 5.3-43-.3-86.6-16.3-126.9M202.8 372.2c1.2.6 2.3 1.2 3.5 1.7l.2.1c-1.2-.6-2.4-1.2-3.7-1.8m266.3-184.6v-.2z" style="fill:url(#kl-firefox-a)"/><radialGradient id="kl-firefox-b" cx="-7667.514" cy="9141.38" r="526.888" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset=".129" style="stop-color:#ffbd4f"/><stop offset=".186" style="stop-color:#ffac31"/><stop offset=".247" style="stop-color:#ff9d17"/><stop offset=".283" style="stop-color:#ff980e"/><stop offset=".403" style="stop-color:#ff563b"/><stop offset=".467" style="stop-color:#ff3750"/><stop offset=".71" style="stop-color:#f5156c"/><stop offset=".782" style="stop-color:#eb0878"/><stop offset=".86" style="stop-color:#e50080"/></radialGradient><path d="M494.1 174.3c-11-26.4-33.3-55-50.7-64 12.4 24.1 21.1 50 25.6 76.7v.5c19.5 55.7 16.7 116.9-7.9 170.6-29 62.2-99.1 125.9-208.8 122.7-118.5-3.4-223-91.4-242.5-206.6-3.6-18.2 0-27.4 1.8-42.2-2.4 11.5-3.8 23.1-4.1 34.9v1.3c.2 139.6 113.6 252.5 253.2 252.3 122.9-.1 227.7-88.5 248.7-209.5.4-3.3.8-6.5 1.1-9.8 5.2-43-.4-86.6-16.4-126.9" style="fill:url(#kl-firefox-b)"/><radialGradient id="kl-firefox-c" cx="-7866.73" cy="8922.242" r="526.888" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset=".3" style="stop-color:#960e18"/><stop offset=".351" style="stop-color:#b11927;stop-opacity:.74"/><stop offset=".435" style="stop-color:#db293d;stop-opacity:.343"/><stop offset=".497" style="stop-color:#f5334b;stop-opacity:9.400000e-02"/><stop offset=".53" style="stop-color:#ff3750;stop-opacity:0"/></radialGradient><path d="M494.1 174.3c-11-26.4-33.3-55-50.7-64 12.4 24.1 21.1 50 25.6 76.7v.5c19.5 55.7 16.7 116.9-7.9 170.6-29 62.2-99.1 125.9-208.8 122.7-118.5-3.4-223-91.4-242.5-206.6-3.6-18.2 0-27.4 1.8-42.2-2.4 11.5-3.8 23.1-4.1 34.9v1.3c.2 139.6 113.6 252.5 253.2 252.3 122.9-.1 227.7-88.5 248.7-209.5.4-3.3.8-6.5 1.1-9.8 5.2-43-.4-86.6-16.4-126.9" style="fill:url(#kl-firefox-c)"/><radialGradient id="kl-firefox-d" cx="-7800.325" cy="9260.909" r="381.667" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset=".132" style="stop-color:#fff44f"/><stop offset=".252" style="stop-color:#ffdc3e"/><stop offset=".506" style="stop-color:#ff9d12"/><stop offset=".526" style="stop-color:#ff980e"/></radialGradient><path d="M371.3 204c.5.4 1.1.8 1.6 1.2-6.3-11.3-14.3-21.6-23.5-30.6C270.8 96 328.8 4.2 338.5-.5l.1-.1c-63.5 37.2-85 106-87 140.4 2.9-.2 5.9-.4 8.9-.4 45.9 0 88.2 24.7 110.8 64.6" style="fill:url(#kl-firefox-d)"/><radialGradient id="kl-firefox-e" cx="-7926.495" cy="8782.792" r="250.858" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset=".353" style="stop-color:#3a8ee6"/><stop offset=".472" style="stop-color:#5c79f0"/><stop offset=".669" style="stop-color:#9059ff"/><stop offset="1" style="stop-color:#c139e6"/></radialGradient><path d="M260.7 219.7c-.4 6.3-22.6 28-30.4 28-71.9 0-83.5 43.5-83.5 43.5 3.2 36.6 28.7 66.8 59.5 82.7 1.4.7 2.8 1.4 4.3 2 2.5 1.1 4.9 2.1 7.4 3 10.6 3.7 21.7 5.9 32.9 6.3 126 5.9 150.4-150.6 59.5-196.1 21.4-2.8 43.2 2.5 60.9 14.8-22.6-39.9-64.9-64.6-110.7-64.7-3 0-5.9.2-8.9.4-25.4 1.7-49.6 11.1-69.6 26.8 3.9 3.3 8.2 7.6 17.4 16.6 17.1 17.1 61.1 34.6 61.2 36.7" style="fill:url(#kl-firefox-e)"/><radialGradient id="kl-firefox-f" cx="-7931.817" cy="8971.409" r="133.026" gradientTransform="matrix(.9545 -.2308 -.27 -1.1175 10267.805 8423.169)" gradientUnits="userSpaceOnUse"><stop offset=".206" style="stop-color:#9059ff;stop-opacity:0"/><stop offset=".278" style="stop-color:#8c4ff3;stop-opacity:6.400000e-02"/><stop offset=".747" style="stop-color:#7716a8;stop-opacity:.45"/><stop offset=".975" style="stop-color:#6e008b;stop-opacity:.6"/></radialGradient><path d="M260.7 219.7c-.4 6.3-22.6 28-30.4 28-71.9 0-83.5 43.5-83.5 43.5 3.2 36.6 28.7 66.8 59.5 82.7 1.4.7 2.8 1.4 4.3 2 2.5 1.1 4.9 2.1 7.4 3 10.6 3.7 21.7 5.9 32.9 6.3 126 5.9 150.4-150.6 59.5-196.1 21.4-2.8 43.2 2.5 60.9 14.8-22.6-39.9-64.9-64.6-110.7-64.7-3 0-5.9.2-8.9.4-25.4 1.7-49.6 11.1-69.6 26.8 3.9 3.3 8.2 7.6 17.4 16.6 17.1 17.1 61.1 34.6 61.2 36.7" style="fill:url(#kl-firefox-f)"/><radialGradient id="kl-firefox-g" cx="-7873.37" cy="9161.301" r="180.498" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset="0" style="stop-color:#ffe226"/><stop offset=".121" style="stop-color:#ffdb27"/><stop offset=".295" style="stop-color:#ffc82a"/><stop offset=".502" style="stop-color:#ffa930"/><stop offset=".732" style="stop-color:#ff7e37"/><stop offset=".792" style="stop-color:#ff7139"/></radialGradient><path d="M170.3 158.2c2 1.3 3.7 2.4 5.2 3.5-5.8-20.2-6-41.5-.7-61.8-23.3 11.3-44 27.3-60.8 47 1.2 0 37.9-.7 56.3 11.3" style="fill:url(#kl-firefox-g)"/><radialGradient id="kl-firefox-h" cx="-7727.279" cy="9280.831" r="770.116" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset=".113" style="stop-color:#fff44f"/><stop offset=".456" style="stop-color:#ff980e"/><stop offset=".622" style="stop-color:#ff5634"/><stop offset=".716" style="stop-color:#ff3647"/><stop offset=".904" style="stop-color:#e31587"/></radialGradient><path d="M9.8 274.3c19.5 115.2 124 203.3 242.5 206.6C362 484 432.1 420.3 461.1 358.2c24.5-53.7 27.3-114.8 7.9-170.6v-.4.4c9 58.5-20.8 115.2-67.4 153.6l-.1.3c-90.7 73.9-177.5 44.6-195 32.6-1.2-.6-2.5-1.2-3.7-1.8-52.9-25.3-74.7-73.4-70-114.8-25.6.4-49.1-14.4-59.9-37.7 28.2-17.3 63.4-18.7 92.9-3.7 29.9 13.6 64 14.9 94.9 3.7-.1-2.1-44.1-19.6-61.2-36.5-9.2-9-13.5-13.4-17.4-16.6-2.1-1.8-4.3-3.4-6.5-4.9-1.5-1-3.2-2.1-5.2-3.5-18.4-12-55.1-11.3-56.3-11.3h-.1c-10-12.7-9.3-54.6-8.7-63.3q-4.5 1.8-8.4 4.5c-8.8 6.3-17.1 13.4-24.7 21.2-8.7 8.7-16.6 18.2-23.7 28.3-16.2 23-27.8 49-33.9 76.6-.3.4-9.2 39.7-4.8 60" style="fill:url(#kl-firefox-h)"/><radialGradient id="kl-firefox-i" cx="-7976.017" cy="9823.985" r="564.057" gradientTransform="matrix(.1031 .9771 .6412 -.06776 -5155.366 8422.637)" gradientUnits="userSpaceOnUse"><stop offset="0" style="stop-color:#fff44f"/><stop offset=".06" style="stop-color:#ffe847"/><stop offset=".168" style="stop-color:#ffc830"/><stop offset=".304" style="stop-color:#ff980e"/><stop offset=".356" style="stop-color:#ff8b16"/><stop offset=".455" style="stop-color:#ff672a"/><stop offset=".57" style="stop-color:#ff3647"/><stop offset=".737" style="stop-color:#e31587"/></radialGradient><path d="M349.4 174.5c9.2 9.1 17.1 19.4 23.5 30.6 1.4 1 2.7 2.1 3.8 3.1C434 261 404 335.7 401.7 341c46.5-38.3 76.3-95 67.3-153.6-28.6-71.3-77.1-100-116.6-162.6-2-3.2-4-6.3-6-9.7-1.1-1.9-2-3.6-2.8-5.2-1.6-3.2-2.9-6.5-3.8-10 0-.3-.2-.6-.6-.7h-.5l-.1.1s-.1.1-.2.1c-9.8 4.6-67.8 96.4 10.8 175z" style="fill:url(#kl-firefox-i)"/><radialGradient id="kl-firefox-j" cx="-7873.37" cy="9094.897" r="480.72" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset=".137" style="stop-color:#fff44f"/><stop offset=".48" style="stop-color:#ff980e"/><stop offset=".592" style="stop-color:#ff5634"/><stop offset=".655" style="stop-color:#ff3647"/><stop offset=".904" style="stop-color:#e31587"/></radialGradient><path d="M376.6 208.3c-1.1-1-2.4-2.1-3.8-3.1-.5-.4-1-.8-1.6-1.2-17.8-12.3-39.5-17.6-60.9-14.8 90.9 45.5 66.5 202-59.5 196.1-11.2-.5-22.3-2.6-32.9-6.3-2.5-.9-4.9-1.9-7.4-3-1.4-.7-2.9-1.3-4.3-2l.2.1c17.6 12 104.3 41.3 195-32.6l.1-.3c2.4-5.4 32.4-80-24.9-132.9" style="fill:url(#kl-firefox-j)"/><radialGradient id="kl-firefox-k" cx="-7747.201" cy="9068.334" r="526.17" gradientTransform="matrix(.982 0 0 -.982 7973.807 9034.763)" gradientUnits="userSpaceOnUse"><stop offset=".094" style="stop-color:#fff44f"/><stop offset=".231" style="stop-color:#ffe141"/><stop offset=".509" style="stop-color:#ffaf1e"/><stop offset=".626" style="stop-color:#ff980e"/></radialGradient><path d="M146.7 291.1s11.7-43.5 83.5-43.5c7.8 0 30-21.7 30.4-28-30.9 11.2-65 9.9-94.9-3.7-29.5-15-64.7-13.6-92.9 3.7 10.8 23.3 34.2 38 59.9 37.7-4.7 41.3 17.2 89.5 70 114.8 1.2.6 2.3 1.2 3.5 1.7-30.8-15.9-56.3-46.1-59.5-82.7" style="fill:url(#kl-firefox-k)"/><linearGradient id="kl-firefox-l" x1="465.416" x2="108.463" y1="440.741" y2="83.722" gradientTransform="matrix(.982 0 0 -.982 -1.496 510.756)" gradientUnits="userSpaceOnUse"><stop offset=".167" style="stop-color:#fff44f;stop-opacity:.8"/><stop offset=".266" style="stop-color:#fff44f;stop-opacity:.634"/><stop offset=".489" style="stop-color:#fff44f;stop-opacity:.217"/><stop offset=".6" style="stop-color:#fff44f;stop-opacity:0"/></linearGradient><path d="M494.1 174.3c-11-26.4-33.3-55-50.7-64 12.4 24.1 21.1 50 25.6 76.7v.4c-28.6-71.2-77-100-116.6-162.5-2-3.2-4-6.3-6-9.7-1.1-1.9-2-3.6-2.8-5.2-1.6-3.2-2.9-6.5-3.8-10 0-.3-.2-.6-.6-.7h-.5l-.1.1s-.1.1-.2.1l.1-.2c-63.5 37.2-85 106-87 140.4 2.9-.2 5.9-.4 8.9-.4 45.8.1 88.1 24.8 110.7 64.7-17.8-12.3-39.5-17.6-60.9-14.8 90.9 45.5 66.5 202-59.5 196.1-11.2-.5-22.3-2.6-32.9-6.3-2.5-.9-4.9-1.9-7.4-3-1.4-.7-2.9-1.3-4.3-2l.2.1c-1.2-.6-2.5-1.2-3.7-1.8 1.2.6 2.3 1.2 3.5 1.7-30.9-16-56.3-46.1-59.5-82.7 0 0 11.7-43.5 83.5-43.5 7.8 0 30-21.7 30.4-28-.1-2.1-44.1-19.6-61.2-36.5-9.2-9-13.5-13.4-17.4-16.6-2.1-1.8-4.3-3.4-6.5-4.9-5.8-20.2-6-41.5-.7-61.8-23.3 11.3-44 27.3-60.8 47h.1c-10-12.7-9.3-54.6-8.7-63.3q-4.5 1.8-8.4 4.5c-8.8 6.3-17.1 13.4-24.7 21.2-8.7 8.8-16.6 18.3-23.6 28.3-16.2 23-27.8 49-33.9 76.6l-.3 1.7c-.5 2.2-2.6 13.5-2.9 15.9-2 11.7-3.2 23.4-3.7 35.2v1.3C8 408 121.4 520.9 261 520.7c122.8-.2 227.6-88.6 248.6-209.6.4-3.3.8-6.5 1.1-9.8 5-43-.6-86.7-16.6-127m-25.1 13v.3z" style="fill:url(#kl-firefox-l)"/></g></svg>';

// The browsers' single-colour marks for the lit button, from Simple Icons (CC0
// 1.0). Their colour marks overlap in gradients and shading that flatten to a
// blot in one ink.
const CHROME_LIT_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="var(--mark-ink, currentColor)" d="M12 0C8.21 0 4.831 1.757 2.632 4.501l3.953 6.848A5.454 5.454 0 0 1 12 6.545h10.691A12 12 0 0 0 12 0zM1.931 5.47A11.943 11.943 0 0 0 0 12c0 6.012 4.42 10.991 10.189 11.864l3.953-6.847a5.45 5.45 0 0 1-6.865-2.29zm13.342 2.166a5.446 5.446 0 0 1 1.45 7.09l.002.001h-.002l-5.344 9.257c.206.01.413.016.621.016 6.627 0 12-5.373 12-12 0-1.54-.29-3.011-.818-4.364zM12 16.364a4.364 4.364 0 1 1 0-8.728 4.364 4.364 0 0 1 0 8.728Z"/></svg>';

const FIREFOX_LIT_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="var(--mark-ink, currentColor)" d="M8.824 7.287c.008 0 .004 0 0 0zm-2.8-1.4c.006 0 .003 0 0 0zm16.754 2.161c-.505-1.215-1.53-2.528-2.333-2.943.654 1.283 1.033 2.57 1.177 3.53l.002.02c-1.314-3.278-3.544-4.6-5.366-7.477-.091-.147-.184-.292-.273-.446a3.545 3.545 0 01-.13-.24 2.118 2.118 0 01-.172-.46.03.03 0 00-.027-.03.038.038 0 00-.021 0l-.006.001a.037.037 0 00-.01.005L15.624 0c-2.585 1.515-3.657 4.168-3.932 5.856a6.197 6.197 0 00-2.305.587.297.297 0 00-.147.37c.057.162.24.24.396.17a5.622 5.622 0 012.008-.523l.067-.005a5.847 5.847 0 011.957.222l.095.03a5.816 5.816 0 01.616.228c.08.036.16.073.238.112l.107.055a5.835 5.835 0 01.368.211 5.953 5.953 0 012.034 2.104c-.62-.437-1.733-.868-2.803-.681 4.183 2.09 3.06 9.292-2.737 9.02a5.164 5.164 0 01-1.513-.292 4.42 4.42 0 01-.538-.232c-1.42-.735-2.593-2.121-2.74-3.806 0 0 .537-2 3.845-2 .357 0 1.38-.998 1.398-1.287-.005-.095-2.029-.9-2.817-1.677-.422-.416-.622-.616-.8-.767a3.47 3.47 0 00-.301-.227 5.388 5.388 0 01-.032-2.842c-1.195.544-2.124 1.403-2.8 2.163h-.006c-.46-.584-.428-2.51-.402-2.913-.006-.025-.343.176-.389.206-.406.29-.787.616-1.136.974-.397.403-.76.839-1.085 1.303a9.816 9.816 0 00-1.562 3.52c-.003.013-.11.487-.19 1.073-.013.09-.026.181-.037.272a7.8 7.8 0 00-.069.667l-.002.034-.023.387-.001.06C.386 18.795 5.593 24 12.016 24c5.752 0 10.527-4.176 11.463-9.661.02-.149.035-.298.052-.448.232-1.994-.025-4.09-.753-5.844z"/></svg>';

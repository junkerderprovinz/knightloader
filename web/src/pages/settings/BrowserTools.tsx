import { useState, type ReactNode } from 'react';
import { buildBookmarklet } from '../../lib/browserTools';
import { useInstallPrompt } from '../../lib/pwaInstall';
import { useT, type TranslationKey } from '../../lib/i18n';
import { Button, Card, SectionTitle } from '../../components/ui';
import { IconDownloads } from '../../lib/icons';

/**
 * Three ways to hand KnightLoader a link from outside the app itself
 * (build-plan.md's 11D): a bookmarklet, the MV3 browser extension
 * (extension/src, downloaded as a zip pre-filled with THIS instance's own
 * address — see internal/api/routes_browsertools.go), and installing this
 * page as an app so the OS Share menu can reach it directly.
 *
 * All three land on the same place, /quickadd (pages/QuickAdd.tsx) — this
 * page only ever has to build the address and the drag-target, not the
 * staging logic.
 *
 * Not the Remote access page build-plan.md section 8's Wave 11 note gives
 * 11C: that page is about reaching this INSTANCE (tokens, QR code, the
 * exposure warning), where this one is about reaching THIS APP from
 * somewhere else. useInstallPrompt (lib/pwaInstall.ts) is shared so 11C's
 * page can offer the identical install button without a second
 * beforeinstallprompt listener stepping on this one — see that file's own
 * doc comment.
 *
 * The strings this page needs are not in en.ts yet - locale files are one
 * writer's lane per wave (11G, phase 3 of this one), same arrangement
 * System.tsx, Diagnostics.tsx, Schedule.tsx and Captcha.tsx already use.
 */
const PENDING = {
  'settings.browsertools.aboutTitle': 'From anywhere else',
  'settings.browsertools.subtitle':
    'Send a link to KnightLoader from anywhere else — another page, another app, or your phone’s Share menu.',
  'settings.browsertools.bookmarkletTitle': 'Bookmarklet',
  'settings.browsertools.bookmarkletHint':
    '1. Drag the button below onto your browser’s bookmarks bar - it saves like any other bookmark, nothing installs. 2. On any page, click it (select some text first if you only want that). A small KnightLoader tab opens with that page’s link, or the selected text, already filled in - review it and add it as a download from there.',
  'settings.browsertools.bookmarkletLink': 'Add to KnightLoader',
  'settings.browsertools.copyCode': 'Copy the code instead',
  'settings.browsertools.copied': 'Copied.',
  'settings.browsertools.extensionTitle': 'Browser extension',
  'settings.browsertools.extensionHint':
    'A right-click menu on any link, selection, or page. The download already points at this instance — nothing to configure. Chromium and Firefox package extensions differently, so pick the one for your browser.',
  'settings.browsertools.downloadChromium': 'Chrome, Edge, Brave (.zip)',
  'settings.browsertools.installChromiumHint':
    'Unzip it, then open chrome://extensions (or edge://extensions, brave://extensions), turn on Developer mode, and choose “Load unpacked” on the unzipped folder.',
  'settings.browsertools.downloadFirefox': 'Firefox (.xpi)',
  'settings.browsertools.installFirefoxHint':
    'Open about:debugging#/runtime/this-firefox, choose “Load Temporary Add-on”, and pick this file. It stays installed until Firefox restarts - a permanent install needs Mozilla’s own signing, which self-hosted software cannot get automatically.',
  'settings.browsertools.installTitle': 'Install as an app',
  'settings.browsertools.installHint':
    'Once installed, your device’s own Share menu can hand a link straight to KnightLoader — no browser tab required.',
  'settings.browsertools.install': 'Install',
  'settings.browsertools.installed': 'Already installed, or this browser offers its own install step in the address bar.',
} as const;

type PendingKey = keyof typeof PENDING;

function useCx() {
  const { t } = useT();
  return (key: PendingKey) => (t(key as unknown as TranslationKey) as string | undefined) ?? PENDING[key];
}

export function BrowserTools() {
  const cx = useCx();
  const origin = window.location.origin;
  const bookmarklet = buildBookmarklet(origin);
  const [copied, setCopied] = useState(false);
  const { available: canInstall, promptInstall } = useInstallPrompt();

  return (
    <div className="flex flex-col gap-6">
      <Card>
        <SectionTitle hue={0}>{cx('settings.browsertools.aboutTitle')}</SectionTitle>
        <p className="text-sm text-carbon-textSub">{cx('settings.browsertools.subtitle')}</p>
      </Card>

      <Card className="flex flex-col gap-3">
        <SectionTitle hue={1}>{cx('settings.browsertools.bookmarkletTitle')}</SectionTitle>
        {/* Normal body text, not a small muted caption - this is a two-step
            how-it-works explanation someone reads once to understand the
            feature at all (jdp: "Ich habe noch nicht verstanden was das
            hier soll und wie das funktionieren soll"), not a passing hint
            beside a control whose purpose is otherwise obvious. */}
        <p className="text-sm text-carbon-textSub">{cx('settings.browsertools.bookmarkletHint')}</p>
        <div className="flex flex-wrap items-center gap-3">
          {/* A real link, not a button with an onClick — dragging IS the
              install step, and only an <a href="javascript:..."> is
              draggable into a bookmarks bar carrying that code. */}
          <a
            href={bookmarklet}
            onClick={(e) => e.preventDefault()}
            className="inline-flex items-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2
              px-3.5 py-2 text-sm font-medium text-carbon-text hover:bg-carbon-surface3"
          >
            <IconDownloads width={15} height={15} />
            {cx('settings.browsertools.bookmarkletLink')}
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
              {copied ? cx('settings.browsertools.copied') : cx('settings.browsertools.copyCode')}
            </Button>
          )}
        </div>
      </Card>

      <Card className="flex flex-col gap-4">
        <SectionTitle hue={2}>{cx('settings.browsertools.extensionTitle')}</SectionTitle>
        <p className="text-sm text-carbon-textSub">{cx('settings.browsertools.extensionHint')}</p>
        {/* Four badges, not two buttons: jdp: "Die downloadbuttons für die
            Browsererweiterung sollen große quadratische Badges sein mit
            jeweils dem Logo des Browsers. es soll für Chrome, Edge, Brave
            und Firefox einen eigenen downloadbadge geben." Chrome/Edge/Brave
            are the same .zip and the same install flow (Chromium's own
            Developer Mode + Load unpacked) - three badges, one download and
            one instruction, not three copies of the same paragraph. Firefox
            gets the .xpi and its own instruction (routes_browsertools.go's
            own doc comment explains why the archive itself is identical
            either way). These are true-color brand marks, not GlimStone's
            own monochrome glyph set - a Firefox logo re-tinted to the
            accent colour would stop reading as Firefox, so they sit outside
            the rainbow/accent system on purpose (the same "semantic colour
            stays semantic" carve-out the design language already makes for
            e.g. a delete action staying red under any accent). */}
        <div className="flex flex-wrap gap-3">
          <BrowserBadge logo={<LogoChrome />} name="Chrome" onClick={downloadZip} />
          <BrowserBadge logo={<LogoEdge />} name="Edge" onClick={downloadZip} />
          <BrowserBadge logo={<LogoBrave />} name="Brave" onClick={downloadZip} />
          <BrowserBadge logo={<LogoFirefox />} name="Firefox" onClick={downloadXpi} />
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <p className="text-[11px] text-carbon-textMuted">{cx('settings.browsertools.installChromiumHint')}</p>
          <p className="text-[11px] text-carbon-textMuted">{cx('settings.browsertools.installFirefoxHint')}</p>
        </div>
      </Card>

      <Card className="flex flex-col gap-3">
        <SectionTitle hue={3}>{cx('settings.browsertools.installTitle')}</SectionTitle>
        <p className="text-[11px] text-carbon-textMuted">{cx('settings.browsertools.installHint')}</p>
        {canInstall ? (
          <div>
            <Button kind="secondary" onClick={() => void promptInstall()}>
              {cx('settings.browsertools.install')}
            </Button>
          </div>
        ) : (
          <p className="text-[11px] text-carbon-textMuted">{cx('settings.browsertools.installed')}</p>
        )}
      </Card>
    </div>
  );
}

function downloadZip() {
  window.location.href = '/api/browser-extension.zip';
}

function downloadXpi() {
  window.location.href = '/api/browser-extension.xpi';
}

/** One large square tile: the browser's own logo, its name, nothing else -
 *  the badge itself is the button. */
function BrowserBadge({ logo, name, onClick }: { logo: ReactNode; name: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={name}
      className="flex h-28 w-28 flex-col items-center justify-center gap-2 rounded-[var(--radius-control)]
        bg-carbon-surface2 transition-colors hover:bg-carbon-surface3"
    >
      <span className="h-14 w-14 shrink-0">{logo}</span>
      <span className="text-xs font-medium text-carbon-text">{name}</span>
    </button>
  );
}

/**
 * Simplified true-colour marks, close in silhouette and palette to each
 * browser's own logo without reproducing its exact artwork - a CSS
 * conic/linear gradient for the brand colours plus one plain SVG glyph on
 * top, not a copy of the trademarked icon file. Kept local to this page:
 * every other icon in lib/icons.tsx is a single-colour `currentColor`
 * glyph in the GlimStone style, and these four are neither - they are
 * fixed-colour brand marks that must NOT flow through the accent/rainbow
 * system (a re-tinted Firefox logo would stop reading as Firefox).
 */
function LogoChrome() {
  return (
    <span
      className="relative block h-full w-full overflow-hidden rounded-full"
      style={{ background: 'conic-gradient(from -90deg, #EA4335 0deg 120deg, #34A853 120deg 240deg, #FBBC05 240deg 360deg)' }}
    >
      <span className="absolute inset-[21%] rounded-full bg-white" />
      <span className="absolute inset-[32%] rounded-full" style={{ background: '#4285F4' }} />
    </span>
  );
}

function LogoEdge() {
  return (
    <span
      className="relative block h-full w-full overflow-hidden rounded-full"
      style={{ background: 'linear-gradient(135deg, #0C59A4 0%, #1B9DE8 45%, #37D48C 100%)' }}
    >
      <svg viewBox="0 0 40 40" className="absolute inset-0 h-full w-full" aria-hidden>
        <path
          d="M6 21C7 12 15 6 23 8c-6 .5-10 4.5-10 9.5 0 4.5 3.5 7.5 8.5 7.5 4 0 7-2 8.8-5.3.6 8.3-6 14.3-14.3 14.3-7.2 0-12.5-5.3-12.5-12.4 0-.2 0-.4.02-.6H6Z"
          fill="#fff"
          opacity=".92"
        />
      </svg>
    </span>
  );
}

function LogoBrave() {
  return (
    <span className="flex h-full w-full items-center justify-center overflow-hidden rounded-full" style={{ background: '#FB542B' }}>
      <svg viewBox="0 0 40 40" className="h-[72%] w-[72%]" aria-hidden>
        <path d="M20 3 33 8v11c0 9-6 15.5-13 18C13 34 7 28 7 19V8Z" fill="#fff" />
        <path d="M20 6.4 30 10.2v8.8c0 7.3-5 12.3-10 14.7-5-2.4-10-7.4-10-14.7v-8.8Z" fill="#FB542B" />
        <circle cx="15.3" cy="17.2" r="1.7" fill="#fff" />
        <circle cx="24.7" cy="17.2" r="1.7" fill="#fff" />
        <path d="M20 21.5c-2 0-3.6.9-4.6 2.3 1-.6 2.3-1 4.6-1s3.6.4 4.6 1c-1-1.4-2.6-2.3-4.6-2.3Z" fill="#fff" />
      </svg>
    </span>
  );
}

function LogoFirefox() {
  return (
    <span
      className="relative block h-full w-full overflow-hidden rounded-full"
      style={{ background: 'radial-gradient(circle at 34% 28%, #FFE900 0%, #FF9100 26%, #FF6611 48%, #E31B23 72%, #9E1533 100%)' }}
    >
      <svg viewBox="0 0 40 40" className="absolute inset-0 h-full w-full" aria-hidden>
        <path
          d="M27.5 12.5c1.6 2 2.5 4.5 2.5 7.3 0 .8-.1 1.6-.2 2.3-1-2.6-3.5-4.4-6.4-4.4-2.6 0-4.8 1.5-5.9 3.7.4-2.6 2.4-4.7 5-5.3-.7-1.3-2-2.2-3.6-2.2-2.2 0-4 1.8-4 4 0 .3 0 .6.1.9-2.6-.9-4.5-3.3-4.5-6.2 0-3.6 2.9-6.5 6.5-6.5 4.4 0 8.2 2.4 10.5 6.4Z"
          fill="#fff"
          opacity=".88"
        />
      </svg>
    </span>
  );
}

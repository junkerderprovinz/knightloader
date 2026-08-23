import { useState } from 'react';
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
        <p className="text-sm text-carbon-textSub">{cx('settings.browsertools.subtitle')}</p>
      </Card>

      <SectionTitle>{cx('settings.browsertools.bookmarkletTitle')}</SectionTitle>
      <Card className="flex flex-col gap-3">
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

      <SectionTitle>{cx('settings.browsertools.extensionTitle')}</SectionTitle>
      <Card className="flex flex-col gap-3">
        <p className="text-sm text-carbon-textSub">{cx('settings.browsertools.extensionHint')}</p>
        {/* Two buttons, not one: a bare zip only ever covered Chromium's own
            "Load unpacked" flow. Firefox's install surfaces (about:addons
            drag-and-drop, about:debugging's "Load Temporary Add-on") look
            for a .xpi specifically - jdp: "Bei JD offizieller Homepage kann
            man für Firefox z.B. eine xpi Datei runterladen". Same archive
            either way (routes_browsertools.go's own doc comment) - only the
            file extension and the instructions beside it differ. */}
        <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap">
          <div className="flex flex-col gap-1.5">
            <Button
              kind="secondary"
              icon={<IconDownloads width={16} height={16} />}
              onClick={() => {
                window.location.href = '/api/browser-extension.zip';
              }}
            >
              {cx('settings.browsertools.downloadChromium')}
            </Button>
            <p className="max-w-xs text-[11px] text-carbon-textMuted">{cx('settings.browsertools.installChromiumHint')}</p>
          </div>
          <div className="flex flex-col gap-1.5">
            <Button
              kind="secondary"
              icon={<IconDownloads width={16} height={16} />}
              onClick={() => {
                window.location.href = '/api/browser-extension.xpi';
              }}
            >
              {cx('settings.browsertools.downloadFirefox')}
            </Button>
            <p className="max-w-xs text-[11px] text-carbon-textMuted">{cx('settings.browsertools.installFirefoxHint')}</p>
          </div>
        </div>
      </Card>

      <SectionTitle>{cx('settings.browsertools.installTitle')}</SectionTitle>
      <Card className="flex flex-col gap-3">
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

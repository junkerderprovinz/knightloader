import { useState, type ReactNode } from 'react';
import logoUrl from '../../assets/logo.svg';
import { buildBookmarklet } from '../../lib/browserTools';
import { useInstallPrompt } from '../../lib/pwaInstall';
import { useT } from '../../lib/i18n';
import { Button, Card, InfoBubble, SectionTitle } from '../../components/ui';

/**
 * Two ways to hand KnightLoader a link from outside the app itself
 * (build-plan.md's 11D): a bookmarklet and the MV3 browser extension
 * (extension/src, downloaded pre-filled with THIS instance's own address -
 * see internal/api/routes_browsertools.go), plus installing this page as an
 * app so the OS Share menu can reach it directly.
 *
 * All three land on the same place, /quickadd (pages/QuickAdd.tsx) - this
 * page only ever has to build the address and the drag-target, not the
 * staging logic.
 *
 * Not the Remote access page build-plan.md section 8's Wave 11 note gives
 * 11C: that page is about reaching this INSTANCE (tokens, QR code, the
 * exposure warning), where this one is about reaching THIS APP from
 * somewhere else. useInstallPrompt (lib/pwaInstall.ts) is shared so 11C's
 * page can offer the identical install button without a second
 * beforeinstallprompt listener stepping on this one - see that file's own
 * doc comment.
 */
export function BrowserTools() {
  const { t } = useT();
  const origin = window.location.origin;
  const bookmarklet = buildBookmarklet(origin);
  const [copied, setCopied] = useState(false);
  const { available: canInstall, promptInstall } = useInstallPrompt();

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
    <div className="flex flex-col gap-6">
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
        <SectionTitle hue={1}>{t('settings.browsertools.extensionTitle')}</SectionTitle>
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
          <BrowserBadge logo={<LogoChrome />} name="Chrome" onClick={downloadZip} hint={chromiumHint} hintLabel={installLabel} />
          <BrowserBadge logo={<LogoEdge />} name="Edge" onClick={downloadZip} hint={chromiumHint} hintLabel={installLabel} />
          <BrowserBadge logo={<LogoBrave />} name="Brave" onClick={downloadZip} hint={chromiumHint} hintLabel={installLabel} />
          <BrowserBadge logo={<LogoOpera />} name="Opera" onClick={downloadZip} hint={chromiumHint} hintLabel={installLabel} />
          <BrowserBadge logo={<LogoVivaldi />} name="Vivaldi" onClick={downloadZip} hint={chromiumHint} hintLabel={installLabel} />
          <BrowserBadge logo={<LogoFirefox />} name="Firefox" onClick={downloadXpi} hint={firefoxHint} hintLabel={installLabel} />
        </div>
      </Card>

      <Card className="flex flex-col gap-3">
        <SectionTitle hue={2}>{t('settings.browsertools.installTitle')}</SectionTitle>
        <p className="text-[11px] text-carbon-textMuted">{t('settings.browsertools.installHint')}</p>
        {canInstall ? (
          <div>
            <Button kind="secondary" onClick={() => void promptInstall()}>
              {t('settings.browsertools.install')}
            </Button>
          </div>
        ) : (
          <p className="text-[11px] text-carbon-textMuted">{t('settings.browsertools.installed')}</p>
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
 *  the badge itself is the button. An optional `hint` renders as an (i)
 *  bubble pinned to the tile's own top-right corner, as a sibling of the
 *  button rather than a child of it - InfoBubble is its own focusable,
 *  hoverable element, and nesting it inside the button would make a click
 *  on the (i) also trigger the download underneath it. */
function BrowserBadge({
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
      <button
        type="button"
        onClick={onClick}
        title={name}
        className="flex h-28 w-28 flex-col items-center justify-center gap-2 rounded-[var(--radius-control)]
          bg-carbon-surface2 transition-colors hover:bg-carbon-surface3"
      >
        <span className="flex h-14 w-14 shrink-0 items-center justify-center">{logo}</span>
        <span className="text-xs font-medium text-carbon-text">{name}</span>
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
 * Real brand marks, taken 1:1 from Simple Icons (simpleicons.org) - never
 * hand-rebuilt. Each is a single flat path tinted with that brand's own
 * published colour, which is Simple Icons' own convention (the full
 * multi-colour Chrome pinwheel etc. is trademarked artwork; the single-hex
 * silhouette is the redistributable form). Kept local to this page: every
 * other icon in lib/icons.tsx is a `currentColor` glyph in the GlimStone
 * style, and these are fixed-colour brand marks that must NOT flow through
 * the accent/rainbow system on purpose (see this file's own top comment).
 */
function LogoChrome() {
  return (
    <svg viewBox="0 0 24 24" className="h-full w-full" fill="#4285F4" aria-hidden>
      <path d="M12 0C8.21 0 4.831 1.757 2.632 4.501l3.953 6.848A5.454 5.454 0 0 1 12 6.545h10.691A12 12 0 0 0 12 0zM1.931 5.47A11.943 11.943 0 0 0 0 12c0 6.012 4.42 10.991 10.189 11.864l3.953-6.847a5.45 5.45 0 0 1-6.865-2.29zm13.342 2.166a5.446 5.446 0 0 1 1.45 7.09l.002.001h-.002l-5.344 9.257c.206.01.413.016.621.016 6.627 0 12-5.373 12-12 0-1.54-.29-3.011-.818-4.364zM12 16.364a4.364 4.364 0 1 1 0-8.728 4.364 4.364 0 0 1 0 8.728Z" />
    </svg>
  );
}

function LogoBrave() {
  return (
    <svg viewBox="0 0 24 24" className="h-full w-full" fill="#FB542B" aria-hidden>
      <path d="M15.68 0l2.096 2.38s1.84-.512 2.709.358c.868.87 1.584 1.638 1.584 1.638l-.562 1.381.715 2.047s-2.104 7.98-2.35 8.955c-.486 1.919-.818 2.66-2.198 3.633-1.38.972-3.884 2.66-4.293 2.916-.409.256-.92.692-1.38.692-.46 0-.97-.436-1.38-.692a185.796 185.796 0 01-4.293-2.916c-1.38-.973-1.712-1.714-2.197-3.633-.247-.975-2.351-8.955-2.351-8.955l.715-2.047-.562-1.381s.716-.768 1.585-1.638c.868-.87 2.708-.358 2.708-.358L8.321 0h7.36zm-3.679 14.936c-.14 0-1.038.317-1.758.69-.72.373-1.242.637-1.409.742-.167.104-.065.301.087.409.152.107 2.194 1.69 2.393 1.866.198.175.489.464.687.464.198 0 .49-.29.688-.464.198-.175 2.24-1.759 2.392-1.866.152-.108.254-.305.087-.41-.167-.104-.689-.368-1.41-.741-.72-.373-1.617-.69-1.757-.69zm0-11.278s-.409.001-1.022.206-1.278.46-1.584.46c-.307 0-2.581-.434-2.581-.434S4.119 7.152 4.119 7.849c0 .697.339.881.68 1.243l2.02 2.149c.192.203.59.511.356 1.066-.235.555-.58 1.26-.196 1.977.384.716 1.042 1.194 1.464 1.115.421-.08 1.412-.598 1.776-.834.364-.237 1.518-1.19 1.518-1.554 0-.365-1.193-1.02-1.413-1.168-.22-.15-1.226-.725-1.247-.95-.02-.227-.012-.293.284-.851.297-.559.831-1.304.742-1.8-.089-.495-.95-.753-1.565-.986-.615-.232-1.799-.671-1.947-.74-.148-.068-.11-.133.339-.175.448-.043 1.719-.212 2.292-.052.573.16 1.552.403 1.632.532.079.13.149.134.067.579-.081.445-.5 2.581-.541 2.96-.04.38-.12.63.288.724.409.094 1.097.256 1.333.256s.924-.162 1.333-.256c.408-.093.329-.344.288-.723-.04-.38-.46-2.516-.541-2.961-.082-.445-.012-.45.067-.579.08-.129 1.059-.372 1.632-.532.573-.16 1.845.009 2.292.052.449.042.487.107.339.175-.148.069-1.332.508-1.947.74-.615.233-1.476.49-1.565.986-.09.496.445 1.241.742 1.8.297.558.304.624.284.85-.02.226-1.026.802-1.247.95-.22.15-1.413.804-1.413 1.169 0 .364 1.154 1.317 1.518 1.554.364.236 1.355.755 1.776.834.422.079 1.08-.4 1.464-1.115.384-.716.039-1.422-.195-1.977-.235-.555.163-.863.355-1.066l2.02-2.149c.341-.362.68-.546.68-1.243 0-.697-2.695-3.96-2.695-3.96s-2.274.436-2.58.436c-.307 0-.972-.256-1.585-.461-.613-.205-1.022-.206-1.022-.206z" />
    </svg>
  );
}

function LogoFirefox() {
  return (
    <svg viewBox="0 0 24 24" className="h-full w-full" fill="#FF7139" aria-hidden>
      <path d="M8.824 7.287c.008 0 .004 0 0 0zm-2.8-1.4c.006 0 .003 0 0 0zm16.754 2.161c-.505-1.215-1.53-2.528-2.333-2.943.654 1.283 1.033 2.57 1.177 3.53l.002.02c-1.314-3.278-3.544-4.6-5.366-7.477-.091-.147-.184-.292-.273-.446a3.545 3.545 0 01-.13-.24 2.118 2.118 0 01-.172-.46.03.03 0 00-.027-.03.038.038 0 00-.021 0l-.006.001a.037.037 0 00-.01.005L15.624 0c-2.585 1.515-3.657 4.168-3.932 5.856a6.197 6.197 0 00-2.305.587.297.297 0 00-.147.37c.057.162.24.24.396.17a5.622 5.622 0 012.008-.523l.067-.005a5.847 5.847 0 011.957.222l.095.03a5.816 5.816 0 01.616.228c.08.036.16.073.238.112l.107.055a5.835 5.835 0 01.368.211 5.953 5.953 0 012.034 2.104c-.62-.437-1.733-.868-2.803-.681 4.183 2.09 3.06 9.292-2.737 9.02a5.164 5.164 0 01-1.513-.292 4.42 4.42 0 01-.538-.232c-1.42-.735-2.593-2.121-2.74-3.806 0 0 .537-2 3.845-2 .357 0 1.38-.998 1.398-1.287-.005-.095-2.029-.9-2.817-1.677-.422-.416-.622-.616-.8-.767a3.47 3.47 0 00-.301-.227 5.388 5.388 0 01-.032-2.842c-1.195.544-2.124 1.403-2.8 2.163h-.006c-.46-.584-.428-2.51-.402-2.913-.006-.025-.343.176-.389.206-.406.29-.787.616-1.136.974-.397.403-.76.839-1.085 1.303a9.816 9.816 0 00-1.562 3.52c-.003.013-.11.487-.19 1.073-.013.09-.026.181-.037.272a7.8 7.8 0 00-.069.667l-.002.034-.023.387-.001.06C.386 18.795 5.593 24 12.016 24c5.752 0 10.527-4.176 11.463-9.661.02-.149.035-.298.052-.448.232-1.994-.025-4.09-.753-5.844z" />
    </svg>
  );
}

function LogoOpera() {
  return (
    <svg viewBox="0 0 24 24" className="h-full w-full" fill="#FF1B2D" aria-hidden>
      <path d="M8.051 5.238c-1.328 1.566-2.186 3.883-2.246 6.48v.564c.061 2.598.918 4.912 2.246 6.479 1.721 2.236 4.279 3.654 7.139 3.654 1.756 0 3.4-.537 4.807-1.471C17.879 22.846 15.074 24 12 24c-.192 0-.383-.004-.57-.014C5.064 23.689 0 18.436 0 12 0 5.371 5.373 0 12 0h.045c3.055.012 5.84 1.166 7.953 3.055-1.408-.93-3.051-1.471-4.81-1.471-2.858 0-5.417 1.42-7.14 3.654h.003zM24 12c0 3.556-1.545 6.748-4.002 8.945-3.078 1.5-5.946.451-6.896-.205 3.023-.664 5.307-4.32 5.307-8.74 0-4.422-2.283-8.075-5.307-8.74.949-.654 3.818-1.703 6.896-.205C22.455 5.25 24 8.445 24 12z" />
    </svg>
  );
}

function LogoVivaldi() {
  return (
    <svg viewBox="0 0 24 24" className="h-full w-full" fill="#EF3939" aria-hidden>
      <path d="M12 0C6.75 0 3.817 0 1.912 1.904.007 3.81 0 6.75 0 12s0 8.175 1.912 10.08C3.825 23.985 6.75 24 12 24c5.25 0 8.183 0 10.088-1.904C23.993 20.19 24 17.25 24 12s0-8.175-1.912-10.08C20.175.015 17.25 0 12 0zm-.168 3a9 9 0 016.49 2.648 9 9 0 010 12.704A9 9 0 1111.832 3zM7.568 7.496a1.433 1.433 0 00-.142.004A1.5 1.5 0 006.21 9.75l1.701 3c.93 1.582 1.839 3.202 2.791 4.822a1.417 1.417 0 001.41.75 1.5 1.5 0 001.223-.81l4.447-7.762A1.56 1.56 0 0018 8.768a1.5 1.5 0 10-2.828.914 2.513 2.513 0 01.256 1.119v.246a2.393 2.393 0 01-2.52 2.13 2.348 2.348 0 01-1.965-1.214c-.307-.51-.6-1.035-.9-1.553-.42-.72-.826-1.41-1.246-2.16a1.433 1.433 0 00-1.229-.754Z" />
    </svg>
  );
}

/**
 * Edge has no Simple Icons entry (it's not in that catalogue) - this is
 * jdp's own SVG (source: svgrepo.com), reproduced whole and unedited rather
 * than approximated, including its own white rounded-square backdrop and
 * three gradients. That backdrop is why this one badge doesn't sit flush
 * like the flat single-colour marks above - it is the logo exactly as
 * given, not a redraw to match them.
 */
function LogoEdge() {
  return (
    <svg viewBox="0 0 512 512" className="h-full w-full" aria-hidden>
      <rect width="512" height="512" rx="15%" fill="#ffffff" />
      <radialGradient id="knightloader-edge-a" cx=".6" cy=".5">
        <stop offset=".8" stopColor="#148" />
        <stop offset="1" stopColor="#137" />
      </radialGradient>
      <radialGradient id="knightloader-edge-b" cx=".5" cy=".6" fx=".2" fy=".6">
        <stop offset=".8" stopColor="#38c" />
        <stop offset="1" stopColor="#269" />
      </radialGradient>
      <linearGradient id="knightloader-edge-c" y1=".5" y2="1">
        <stop offset=".1" stopColor="#5ad" />
        <stop offset=".6" stopColor="#5c8" />
        <stop offset=".8" stopColor="#7d5" />
      </linearGradient>
      <path
        d="M439 374c-50 77-131 98-163 96-191-9-162-262-47-261-82 52 30 224 195 157 17-12 20 3 15 8"
        fill="url(#knightloader-edge-a)"
      />
      <path
        d="M311 255c18-82-31-135-129-135S38 212 38 259c0 124 125 253 287 203-134 39-214-116-146-210 46-66 123-68 132 3"
        fill="url(#knightloader-edge-b)"
      />
      <path
        d="M39 253C51-15 419-30 472 202c14 107-86 149-166 115-42-26 26-20-3-99-48-112-251-103-264 35"
        fill="url(#knightloader-edge-c)"
      />
    </svg>
  );
}

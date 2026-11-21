import { useState } from 'react';
import { useT } from '../../lib/i18n';
import { Card, ErrorCard, SectionTitle } from '../../components/ui';
import { IconDiagnostics, IconDiscord, IconGithub } from '../../lib/icons';
import { COFFEE_BUTTON_SVG, MANUAL_SVG } from '../../lib/appMarks';
import { IconBitcoin, IconPayPal } from '../../components/donateMarks';
import { BrandMark, ReadmeButton } from '../../components/ReadmeButton';
import { CoffeeDialog } from '../../components/CoffeeDialog';
import { CryptoDonateDialog } from '../../components/CryptoDonateDialog';
import { PaypalDialog } from '../../components/PaypalDialog';
import { PAYPAL_PAGE } from '../../lib/donate';
import { fetchDeploymentInfo } from '../../lib/api';
import { openExternal, popupsWork } from '../../lib/external';
import { useResource } from '../../lib/useResource';
import { useToast } from '../../lib/toast';
import { saveDiagnostics } from './diagnostics/bundle';
import { ComponentsCard } from './help/Components';
import { MANUAL_URL, REPO_URL, VersionCard } from './help/VersionCard';

/**
 * Help draws the Info tile, the last one in Settings, with GlimStone's four
 * cards in their order: KnightLoader's version and updates, every component
 * it starts, the ways to report something and who made the app. What the app
 * can do is told by the manual, which the Help card opens.
 */
export function Help() {
  const { t } = useT();
  // Both version cards wait for how this instance runs rather than flash the
  // wrong route or rows.
  const { data: deployment, failed, reload } = useResource(fetchDeploymentInfo);
  return (
    <div data-new="info-tile" className="flex flex-col gap-10">
      {/* The hues number the cards in draw order; renumber when adding one. */}
      {failed && <ErrorCard message={t('common.loadFailed')} retry={reload} retryLabel={t('common.retry')} />}
      {deployment && <VersionCard deployment={deployment} hue={0} />}
      {deployment && <ComponentsCard deployment={deployment} hue={1} />}
      <Report hue={2} />
      <About hue={3} />
    </div>
  );
}

const DISCORD_URL = 'https://discord.gg/2CTZyKaFb6';

/**
 * About says who made the app and on what terms, then offers the ways to give.
 * It is the last card of the Info tile. Its buttons are the README's
 * (GlimStone's "The App tab"), one line each, every one with its mark.
 */
export function About({ hue }: { hue: number }) {
  const { t } = useT();
  // One window at a time, and each mounts only while open, so nothing from
  // BMAC or PayPal loads before somebody asks for it.
  const [giving, setGiving] = useState<'coffee' | 'paypal' | 'crypto' | null>(null);
  const stopGiving = () => setGiving(null);
  // The window's wallet button logs in through a popup. Where the webview
  // cannot open one, PayPal's own donation page does the whole job.
  const givePaypal = async () => {
    if (await popupsWork()) setGiving('paypal');
    else openExternal(PAYPAL_PAGE);
  };
  return (
    <Card hue={hue} className="flex flex-col gap-3">
      <SectionTitle>{t('settings.about.title')}</SectionTitle>
      <p className="text-sm text-carbon-textSub">{t('settings.about.body')}</p>
      <p className="text-sm text-carbon-textSub">{t('settings.about.coffee')}</p>
      {/* The ways to give share one row, a blank line below their sentence: the
          hosted payments first, the wallet last. Each opens its own window in
          the app. Buy Me a Coffee wears its own artwork, whose lettering
          carries the words, so its name is the accessible one. */}
      <div className="glim-readme-btn-rows glim-about-give">
        <ReadmeButton
          brand="coffee"
          parts={[{ name: t('settings.about.coffeeButton'), onClick: () => setGiving('coffee') }]}
          art={<BrandMark svg={COFFEE_BUTTON_SVG} />}
        />
        <ReadmeButton
          brand="paypal"
          parts={[{ name: t('settings.about.paypal'), onClick: () => void givePaypal() }]}
          mark={<IconPayPal />}
          markClass="glim-paypal-mark"
        />
        {/* The bare letterform, which a mark class can paint; the coin tiles
            in the window keep the disc. */}
        <ReadmeButton
          brand="bitcoin"
          parts={[{ name: t('settings.about.crypto'), onClick: () => setGiving('crypto') }]}
          mark={<IconBitcoin />}
          markClass="glim-bitcoin-mark"
        />
      </div>
      {giving === 'coffee' && <CoffeeDialog onClose={stopGiving} />}
      {giving === 'paypal' && <PaypalDialog onClose={stopGiving} />}
      {giving === 'crypto' && <CryptoDonateDialog onClose={stopGiving} />}
    </Card>
  );
}

/**
 * Report holds the ways to report something: the repository, the Discord
 * server, the manual and the diagnostics bundle as a file to attach to an
 * issue. The sentence sits directly above the buttons it asks for. The glyphs
 * are drawn tight, so they fill the mark's box as the book does, which comes
 * cropped to its ink.
 */
function Report({ hue }: { hue: number }) {
  const { t } = useT();
  const { toast } = useToast();

  async function onBugReport() {
    try {
      await saveDiagnostics();
    } catch (e) {
      toast(t('settings.diagnostics.downloadFailed', { error: String(e).replace(/^(Error|ApiError):\s*/, '') }), 'fail');
    }
  }

  return (
    <Card hue={hue} className="flex flex-col gap-3">
      <SectionTitle hint={t('settings.info.helpHint')}>{t('settings.info.helpTitle')}</SectionTitle>
      <p className="text-sm text-carbon-textSub">{t('settings.about.report')}</p>
      <div className="glim-readme-btn-rows">
        {/* Links, so middle-click and copy-link work. */}
        <ReadmeButton
          brand="github"
          parts={[{ name: t('settings.about.github'), href: REPO_URL }]}
          mark={<IconGithub tight />}
          markClass="glim-github-mark"
        />
        <ReadmeButton
          brand="discord"
          parts={[{ name: t('settings.about.discord'), href: DISCORD_URL }]}
          mark={<IconDiscord tight />}
          markClass="glim-discord-mark"
        />
        {/* The manual wears the README's docs button, its book and its
            yellow, and the book opens under the pointer. */}
        <ReadmeButton
          brand="docs"
          data-new="manual-button"
          parts={[{ name: t('settings.info.manual'), sub: t('settings.info.manualOpen'), href: MANUAL_URL }]}
          mark={<BrandMark svg={MANUAL_SVG} />}
          markClass="glim-docs-mark"
        />
        {/* The file goes with an issue on GitHub, so it lights up beside it. */}
        <ReadmeButton
          brand="github"
          parts={[
            {
              name: t('settings.info.bugReport'),
              sub: t('settings.info.bugReportDownload'),
              onClick: () => void onBugReport(),
            },
          ]}
          mark={<IconDiagnostics tight />}
        />
      </div>
    </Card>
  );
}

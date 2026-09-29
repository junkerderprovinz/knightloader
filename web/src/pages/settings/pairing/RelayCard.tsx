// RelayCard picks how the instances of a group reach each other: the project
// relay, an own relay, or none. It leads with what a relay is for and whether
// it is connected; each route gets one sentence and what it needs. "How does
// it work?" opens three short sections: the route picture, what the relay
// sees next to what it never does, and the encryption underneath both. Every
// control saves on its own, as the rest of the settings do.
import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { Button, Card, IconBadge, InfoBubble, LabelBadge, SectionTitle, TextInput, ToggleRow } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { saveRelayConfig, type ConnectInfo, type RelayConfig, type RelayMode } from '../../../lib/api';
import { copyToClipboard } from '../../../lib/clipboard';
import { useT, type TranslationKey } from '../../../lib/i18n';
import {
  IconCheckDrawn,
  IconChevronDown,
  IconChevronUp,
  IconClipboard,
  IconEye,
  IconEyeOff,
  IconLock,
  IconSearch,
  IconShieldCheck,
} from '../../../lib/icons';
import { useToast } from '../../../lib/toast';
import { LegendKey, LegendMessage, RouteDiagram, RouteGlyph } from './pairingArt';

type T = ReturnType<typeof useT>['t'];

const MODES: RelayMode[] = ['project', 'own', 'off'];

// An own relay without TLS still works on a LAN, but its relay key then
// travels in the clear, so the card says so.
const PLAINTEXT_RELAY = /^(ws|http):\/\//i;

/**
 * The command that starts a relay. The relay keeps nothing across a restart,
 * which is why neither this nor Dockerfile.relay declares a volume.
 */
export const RELAY_RUN_COMMAND =
  'docker run -d --name knightloader-relay -p 8760:8760 --restart unless-stopped ghcr.io/junkerderprovinz/knightloader-relay:latest';

const COPY: Record<
  RelayMode,
  { name: TranslationKey; sentence: TranslationKey; need?: TranslationKey; sees: TranslationKey; alt: TranslationKey }
> = {
  project: {
    name: 'relay.project',
    sentence: 'relay.projectSentence',
    need: 'relay.projectNeed',
    sees: 'relay.projectSees',
    alt: 'relay.projectAlt',
  },
  // No `need`: the two source tiles right below say what an own relay takes,
  // in more detail than one line could.
  own: {
    name: 'relay.own',
    sentence: 'relay.ownSentence',
    sees: 'relay.ownSees',
    alt: 'relay.ownAlt',
  },
  off: {
    name: 'relay.off',
    sentence: 'relay.offSentence',
    need: 'relay.offNeed',
    sees: 'relay.offSees',
    alt: 'relay.offAlt',
  },
};

function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

function Fact({ glyph, label, text }: { glyph: ReactNode; label?: string; text: ReactNode }) {
  return (
    <p className="flex items-start gap-2 text-sm text-carbon-textSub">
      <span className="mt-0.5 shrink-0 text-carbon-textMuted [&>svg]:h-4 [&>svg]:w-4">{glyph}</span>
      <span>
        {label && <strong className="font-semibold text-carbon-text">{label} </strong>}
        {text}
      </span>
    </p>
  );
}

/** One of the two places an own relay can come from. */
function Source({
  icon,
  onAccent,
  name,
  sub,
  children,
  address,
  t,
}: {
  icon: ReactNode;
  onAccent?: boolean;
  name: string;
  sub: string;
  children: ReactNode;
  address: string;
  t: T;
}) {
  return (
    <div className="flex flex-col gap-2.5 rounded-[var(--radius-control)] bg-carbon-surface2 px-4 pb-4 pt-3.5">
      <div className="flex items-center gap-3">
        <span
          className={`grid h-11 w-11 shrink-0 place-items-center rounded-[var(--radius-control)] ${
            onAccent ? 'bg-accent text-accentContrast' : 'bg-carbon-surface3 text-carbon-text'
          }`}
        >
          {icon}
        </span>
        <span className="min-w-0">
          <span className="block text-sm font-semibold text-carbon-text">{name}</span>
          <span className="block text-xs text-carbon-textSub">{sub}</span>
        </span>
      </div>
      {children}
      <p className="mt-auto pt-0.5 text-xs text-carbon-textMuted">
        {t('relay.addressAfter')}
        <span dir="ltr" className="block break-all font-mono text-carbon-textSub">
          {address}
        </span>
      </p>
    </div>
  );
}

function CommandLine({ t }: { t: T }) {
  const [copies, setCopies] = useState(0);
  return (
    <div className="flex items-start gap-2">
      <code
        className="glim-num min-w-0 flex-1 overflow-x-auto rounded-[var(--radius-control)] bg-carbon-surface px-3 py-2 text-xs leading-relaxed text-carbon-text"
        dir="ltr"
      >
        {RELAY_RUN_COMMAND}
      </code>
      <IconBadge
        labelled
        icon={copies > 0 ? <IconCheckDrawn width={16} height={16} /> : <IconClipboard width={16} height={16} />}
        title={t('common.copy')}
        aria-label={t('common.copy')}
        confirm={copies}
        onClick={async () => {
          if (await copyToClipboard(RELAY_RUN_COMMAND)) setCopies((n) => n + 1);
        }}
      />
    </div>
  );
}

export function RelayCard({
  group,
  relay,
  onRelay,
  hue,
}: {
  group: ConnectInfo;
  relay: RelayConfig;
  /** Called with what the server stored, so the page reloads the group. */
  onRelay: (r: RelayConfig) => void;
  hue?: number;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [mode, setMode] = useState<RelayMode>(relay.mode);
  const [url, setUrl] = useState(relay.relayUrl);
  const [urlError, setUrlError] = useState<string | null>(null);
  const [howOpen, setHowOpen] = useState(false);
  const howId = useId();
  const editing = useRef(false);
  const timer = useRef<number | null>(null);

  // The page reloads the relay every few seconds; that must not undo what is
  // being typed or picked right now.
  useEffect(() => {
    if (!editing.current) setUrl(relay.relayUrl);
  }, [relay.relayUrl]);
  useEffect(() => {
    setMode(relay.mode);
  }, [relay.mode]);
  useEffect(
    () => () => {
      if (timer.current !== null) window.clearTimeout(timer.current);
    },
    [],
  );

  async function save(patch: { mode?: RelayMode; url?: string; serve?: boolean }): Promise<boolean> {
    try {
      onRelay(await saveRelayConfig(patch.url ?? url, undefined, patch.serve, patch.mode));
      return true;
    } catch (e) {
      const msg = e instanceof Error && e.message ? e.message : t('relay.saveError');
      if (patch.url !== undefined) setUrlError(msg);
      else toast(msg, 'fail');
      return false;
    }
  }

  function pick(next: RelayMode) {
    setMode(next);
    void save({ mode: next });
  }

  function typeUrl(v: string) {
    editing.current = true;
    setUrl(v);
    setUrlError(null);
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      void save({ url: v.trim() }).then(() => {
        editing.current = false;
      });
    }, 800);
  }

  const copy = COPY[mode];
  // The relay key comes from the twelve words, so the relay is only dialled
  // inside a group; before that there is nothing to be connected.
  const state =
    mode === 'off' ? (
      <LabelBadge label={t('relay.off')} />
    ) : !group.active ? (
      <LabelBadge label={t('relay.noGroup')} />
    ) : relay.connected ? (
      <LabelBadge label={t('instances.connected')} tone="ok" />
    ) : (
      <LabelBadge label={t('instances.notConnected')} tone="fail" />
    );

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('relay.hint')}>{t('relay.title')}</SectionTitle>
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <p className="min-w-0 flex-[1_1_16rem] text-[15px] text-carbon-text">{t('relay.lead')}</p>
        <span data-testid="relay-state">{state}</span>
      </div>
      {mode !== 'off' && !group.active && <p className="text-sm text-carbon-textSub">{t('relay.noGroupLine')}</p>}

      <Tabs
        variant="well"
        labelled
        label={t('relay.title')}
        active={mode}
        onSelect={(id) => pick(id as RelayMode)}
        items={MODES.map((m) => ({ id: m, label: t(COPY[m].name), icon: <RouteGlyph kind={m} /> }))}
      />

      <div className="flex flex-col gap-2">
        <p className="text-sm text-carbon-textSub">
          {mode === 'project' ? t(copy.sentence, { host: hostOf(group.projectRelayUrl) }) : t(copy.sentence)}
        </p>
        {copy.need && <Fact glyph={<IconClipboard />} label={t('relay.needLabel')} text={t(copy.need)} />}
      </div>

      {mode === 'own' && (
        <div className="flex flex-col gap-3.5" data-testid="own-relay">
          <span className="glim-eyebrow">{t('relay.sourcesTitle')}</span>
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            <Source
              icon={<RouteGlyph kind="container" />}
              onAccent
              name={t('relay.containerName')}
              sub={t('relay.containerSub')}
              address="https://relay.example.org"
              t={t}
            >
              <Fact glyph={<IconSearch />} text={t('relay.containerFind', { name: t('relay.containerName') })} />
              <CommandLine t={t} />
              <Fact glyph={<IconShieldCheck />} text={t('relay.containerCert')} />
            </Source>
            <Source
              icon={<RouteGlyph kind="server" />}
              name={t('relay.instanceName')}
              sub={t('relay.instanceSub')}
              address="https://knightloader.example.org"
              t={t}
            >
              <div className="rounded-[var(--radius-control)] bg-carbon-surface px-3 py-2">
                <ToggleRow
                  label={t('relay.serve')}
                  hint={t('relay.serveHint')}
                  checked={relay.serve}
                  onChange={(v) => void save({ serve: v })}
                />
                {relay.serve && (
                  <p className="mt-1 text-xs text-carbon-textMuted">{t('relay.serveClients', { count: relay.serveClients })}</p>
                )}
              </div>
              <Fact
                glyph={<IconShieldCheck />}
                text={
                  <>
                    {t('relay.instanceCert')} <InfoBubble tip={t('relay.instanceCertTip')} />
                  </>
                }
              />
            </Source>
          </div>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="relay-address" className="inline-flex items-center gap-1.5 text-xs font-semibold text-carbon-textSub">
              {t('relay.addressLabel')}
              <InfoBubble tip={t('relay.addressTip')} />
            </label>
            <TextInput
              id="relay-address"
              type="url"
              value={url}
              onChange={(e) => typeUrl(e.target.value)}
              spellCheck={false}
              autoComplete="off"
              placeholder="https://relay.example.org"
              dir="ltr"
              className="font-mono"
            />
            {urlError && <p className="text-sm text-statusFail">{urlError}</p>}
            {!urlError && PLAINTEXT_RELAY.test(url.trim()) && (
              <p className="rounded-[var(--radius-control)] bg-statusWarnBgSoft px-3 py-2.5 text-sm leading-relaxed text-carbon-text">
                {t('relay.plaintextWarning')}
              </p>
            )}
          </div>
        </div>
      )}

      <div>
        <Button
          kind="secondary"
          icon={howOpen ? <IconChevronUp /> : <IconChevronDown />}
          onClick={() => setHowOpen((v) => !v)}
          aria-expanded={howOpen}
          aria-controls={howId}
        >
          {t('relay.howItWorks')}
        </Button>
      </div>

      {howOpen && (
        <div id={howId} className="flex flex-col gap-5 border-t border-carbon-border pt-4">
          <section className="flex flex-col gap-3">
            <h4 className="glim-eyebrow">{t('relay.howDoesTitle')}</h4>
            <div className="grid grid-cols-1 items-center gap-5 md:grid-cols-2 md:gap-10">
              <RouteDiagram
                mode={mode}
                alt={t(copy.alt)}
                labels={{
                  relay: t('relay.diagramRelay'),
                  projectHost: hostOf(group.projectRelayUrl),
                  ownAddress: t('relay.diagramOwnAddress'),
                  yourNetwork: t('relay.diagramYourNetwork'),
                  otherNetwork: t('relay.diagramOtherNetwork'),
                  yourLan: t('relay.diagramYourNetwork'),
                }}
              />
              <div className="flex flex-wrap gap-x-5 gap-y-1.5 text-xs text-carbon-textSub">
                <span className="inline-flex items-center gap-2">
                  <LegendKey />
                  {t('relay.legendKey')}
                </span>
                <span className="inline-flex items-center gap-2">
                  <LegendMessage />
                  {t('relay.legendMessage')}
                </span>
              </div>
            </div>
          </section>

          {/* Sees and does not see side by side, so the one honest limit, the
              routing metadata, sits right next to everything it does not
              cost. */}
          <section className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 p-3.5">
              <span className="inline-flex items-center gap-2 text-sm font-semibold text-carbon-text [&>svg]:h-4 [&>svg]:w-4">
                <IconEye />
                {t('relay.seesLabel')}
              </span>
              <p className="text-sm text-carbon-textSub">{t(copy.sees)}</p>
            </div>
            <div className="flex flex-col gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 p-3.5">
              <span className="inline-flex items-center gap-2 text-sm font-semibold text-carbon-text [&>svg]:h-4 [&>svg]:w-4">
                <IconEyeOff />
                {t('relay.notSeesLabel')}
              </span>
              <ul className="flex list-disc flex-col gap-1 ps-4 text-sm text-carbon-textSub">
                <li>{t('relay.notSeesContent')}</li>
                <li>{t('relay.notSeesFiles')}</li>
              </ul>
            </div>
          </section>

          <section className="flex flex-col gap-2">
            <h4 className="glim-eyebrow">{t('relay.encryptionTitle')}</h4>
            <p className="flex items-start gap-2.5 text-sm text-carbon-textSub">
              <span className="mt-0.5 shrink-0 text-accentInk [&>svg]:h-4 [&>svg]:w-4">
                <IconLock />
              </span>
              <span>
                {t('relay.e2e')} <InfoBubble tip={t('relay.e2eTip')} />
              </span>
            </p>
          </section>
        </div>
      )}
    </Card>
  );
}

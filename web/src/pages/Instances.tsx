import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { type Instance, type Settings, fetchInstances, fetchSettings, addInstance, removeInstance, redeemPairingCode } from '../lib/api';
import { useT } from '../lib/i18n';
import { PageHeader, Card, Button, Field, IconBadge, TextInput, SectionTitle } from '../components/ui';
import { InstanceCard } from '../components/InstanceCard';
import { FirstTouchHint } from '../components/FirstTouchHint';
import { IconClipboard } from '../lib/icons';
import { useToast } from '../lib/toast';

// Reading the clipboard, unlike writing it (lib/clipboard.ts's copyToClipboard),
// has no reliable fallback for an insecure origin - execCommand('copy') works
// everywhere, but browsers refuse a script-driven execCommand('paste') outright
// as a real security boundary, not merely an availability quirk. Feature-detected
// once at module scope (the same check PasteFromClipboardButton.tsx already
// established for exactly this API), then used to DISABLE the paste badge
// below rather than hide it - a control that vanishes teaches nobody what the
// mode can do (settings/Look.tsx's UpdateCard/SystemCards established that
// pattern first). `disabled` carries the "why" in its own title instead of
// pretending the option was never there, which is the honest response to a
// real, unworkaroundable browser boundary - KnightLoader's most common real
// deployment is plain http://<lan-ip>, an insecure context, where this is
// always false.
const CLIPBOARD_READABLE = typeof navigator !== 'undefined' && !!navigator.clipboard?.readText;

export function Instances() {
  const { t } = useT();
  const { toast } = useToast();
  const [peers, setPeers] = useState<Instance[]>([]);
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [err, setErr] = useState('');
  const [code, setCode] = useState('');
  const [pairing, setPairing] = useState(false);
  const [pairErr, setPairErr] = useState('');
  const [pairOk, setPairOk] = useState('');
  // The configured name (settings/Access.tsx's own IdentityCard), so this
  // instance shows up on its own card the same way a peer does - not the
  // generic "this instance" placeholder (jdp: "unter instanz soll diese
  // instanz mit dem eingestellten namen erscheinen nicht mit 'diese
  // instanz'"). Falls back to the placeholder for the common case of never
  // having named it.
  const [ownName, setOwnName] = useState('');
  const navigate = useNavigate();

  const load = () => fetchInstances().then(setPeers);
  useEffect(() => {
    load();
    fetchSettings()
      .then((s: Settings) => setOwnName(s.instanceName))
      .catch(() => {});
  }, []);

  async function onAdd() {
    setErr('');
    try {
      const r = await addInstance(name.trim(), url.trim());
      if (!r.online) setErr(t('instances.offlineWarning'));
      setName('');
      setUrl('');
      await load();
    } catch (e: any) {
      setErr(String(e?.message ?? e));
    }
  }

  // Redeem a code from the OTHER instance's own Access tab (settings/Access.tsx's
  // own "Generate pairing code" card, backed by POST /api/instances/pairing-code)
  // instead of typing that instance's name and address here by hand - one call
  // registers both directions (internal/api/routes_pairing.go's own doc comment
  // on why the redeem handler completes the other side before adding it locally).
  async function onPair() {
    setPairErr('');
    setPairOk('');
    setPairing(true);
    try {
      const r = await redeemPairingCode(code.trim());
      setPairOk(t('instances.pairSuccess', { name: r.name }));
      setCode('');
      await load();
    } catch (e: any) {
      setPairErr(String(e?.message ?? e));
    } finally {
      setPairing(false);
    }
  }

  async function onRemove(n: string) {
    await removeInstance(n);
    await load();
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Subtitle removed (jdp, 2026-08-24: "text entfernen: Alle
          KnightLoader von einer Oberfläche aus sehen und steuern.") - the
          title alone already says what this page is. */}
      <PageHeader title={t('instances.title')} />

      <FirstTouchHint id="instances" />

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <InstanceCard name={ownName || t('instances.thisInstance')} url={location.host} base="/api" />
        {peers.map((p, i) => (
          <InstanceCard
            key={p.name}
            name={p.displayName ?? p.name}
            url={p.url}
            relayId={p.relayId}
            base={`/api/instances/${encodeURIComponent(p.name)}`}
            onOpen={() => navigate(`/downloads?instance=${encodeURIComponent(p.name)}`)}
            onRemove={() => onRemove(p.name)}
            hue={i}
          />
        ))}
      </div>

      <Card className="flex flex-col gap-4">
        <SectionTitle>{t('instances.add')}</SectionTitle>
        <div className="grid grid-cols-1 sm:grid-cols-[1fr_2fr_auto] gap-3 items-end">
          <Field label={t('instances.name')}>
            <TextInput placeholder="cellar" value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={t('instances.url')}>
            <TextInput placeholder="http://host:8749" value={url} onChange={(e) => setUrl(e.target.value)} />
          </Field>
          <Button kind="secondary" onClick={onAdd} disabled={!name.trim() || !url.trim()}>
            {t('instances.addButton')}
          </Button>
        </div>
        {err && <div className="text-statusFail text-sm">{err}</div>}
      </Card>

      <Card className="flex flex-col gap-3">
        <SectionTitle hint={t('instances.pairHint')}>{t('instances.pairTitle')}</SectionTitle>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          {/* Same TextInput the Name field above uses (jdp, 2026-08-25:
              "das eingabefeld für den Pairing-code ... soll exakt gleich
              hoch und formatiert sein wie ... das Eingabefeld für den
              Namen") - a hand-built well (a div padded around a bare
              <input>) LOOKS like it should match TextInput's own
              px-3/py-2/text-sm, but a native <input>'s own box-model
              quirks are exactly why that component exists instead of every
              caller re-deriving the same three classes; dir="ltr" and a
              monospace class are just props now, not a reason to opt out
              of it. h-9 on all three controls below (the input, the paste
              badge, the button) is the second half of the same request -
              IconBadge's own square footprint is a fixed h-8 everywhere
              else it is used, which reads a few px short beside a
              text-sm/py-2 input and button once the three sit in one row. */}
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <TextInput
              dir="ltr"
              placeholder={t('instances.pairPlaceholder')}
              value={code}
              onChange={(e) => setCode(e.target.value)}
              className="glim-num h-9 min-w-0 flex-1"
            />
            <IconBadge
              icon={<IconClipboard width={14} height={14} />}
              title={CLIPBOARD_READABLE ? t('instances.pairPaste') : t('instances.pairPasteUnavailable')}
              aria-label={CLIPBOARD_READABLE ? t('instances.pairPaste') : t('instances.pairPasteUnavailable')}
              disabled={!CLIPBOARD_READABLE}
              className="h-9 w-9 shrink-0"
              onClick={async () => {
                try {
                  const text = (await navigator.clipboard.readText()).trim();
                  if (text) setCode(text);
                } catch (e) {
                  toast(t('list.failed', { error: String(e) }), 'fail');
                }
              }}
            />
          </div>
          <Button kind="secondary" className="h-9" onClick={() => void onPair()} disabled={!code.trim() || pairing}>
            {t('instances.pairButton')}
          </Button>
        </div>
        {pairErr && <div className="text-statusFail text-sm">{pairErr}</div>}
        {pairOk && <div className="text-statusOk text-sm">{pairOk}</div>}
      </Card>
    </div>
  );
}

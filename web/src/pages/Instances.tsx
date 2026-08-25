import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { type Instance, fetchInstances, addInstance, removeInstance, redeemPairingCode } from '../lib/api';
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
  const navigate = useNavigate();

  const load = () => fetchInstances().then(setPeers);
  useEffect(() => {
    load();
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
        <InstanceCard name={t('instances.thisInstance')} url={location.host} base="/api" />
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
          {/* Same "this holds a code" treatment as the generated-code display
              on the OTHER instance's own Access tab (settings/Access.tsx's
              PairingCard) - monospace, ltr, a surface2 well - rather than a
              plain multi-line textarea that gave no visual hint this field
              specifically wants a pasted code (jdp, 2026-08-24: "schönes
              eingabefeld bitte machen"). A single-line <input>, not TextArea:
              a pasted code is one line, and PairingCard's own read-only
              <code> display is the height this now matches (jdp: "eingabefeld
              soll normalgroß sein, jetzt ist es zu groß" - the rows={2}
              TextArea this replaced rendered as a two-row block for a value
              that is never more than one line). */}
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <div className="min-w-0 flex-1 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2">
              <input
                type="text"
                dir="ltr"
                placeholder={t('instances.pairPlaceholder')}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                className="glim-num w-full border-0 bg-transparent p-0 text-sm text-carbon-text placeholder:text-carbon-textMuted outline-none"
              />
            </div>
            <IconBadge
              icon={<IconClipboard width={14} height={14} />}
              title={CLIPBOARD_READABLE ? t('instances.pairPaste') : t('instances.pairPasteUnavailable')}
              aria-label={CLIPBOARD_READABLE ? t('instances.pairPaste') : t('instances.pairPasteUnavailable')}
              disabled={!CLIPBOARD_READABLE}
              className="shrink-0"
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
          <Button kind="secondary" onClick={() => void onPair()} disabled={!code.trim() || pairing}>
            {t('instances.pairButton')}
          </Button>
        </div>
        {pairErr && <div className="text-statusFail text-sm">{pairErr}</div>}
        {pairOk && <div className="text-statusOk text-sm">{pairOk}</div>}
      </Card>
    </div>
  );
}

// The accounts page's card for the own Usenet servers. A server is a settings
// row plus a sealed login (internal/api/routes_usenetservers.go), so it is
// edited here through its own routes rather than the catalogue's credential
// dialog: it has a host, a port and limits that no debrid key has. The Add
// button the page hands in opens the window for a new one through `adding`,
// which starts on the tiles of lib/usenetProviders.ts.
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import {
  REDACTED_HEADER,
  type UsenetServerRow,
  type UsenetServerSave,
  deleteUsenetServer,
  fetchUsenetServers,
  saveUsenetServer,
  testUsenetServer,
} from '../lib/api';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { IconBack, IconCheck, IconClose, IconGlobe, IconSave, IconTrash } from '../lib/icons';
import { USENET_PROVIDERS, type UsenetProvider } from '../lib/usenetProviders';
import { AccountTable } from './AccountTable';
import { BrandPicker, PickerSearch, brandMatches, type Brand } from './BrandPicker';
import { TestButton } from './TestButton';
import { Button, EmptyState, InfoBubble, Modal, NumberInput, PasswordInput, SectionTitle, TextInput, ToggleRow, type ButtonVerdict } from './ui';
import { SettingRow } from '../pages/settings/controls';

/** A server's last test on this page, kept until the page is left. */
type Checked = { ok: boolean; detail: string };

const BLANK: UsenetServerSave = {
  id: '',
  host: '',
  port: 563,
  tls: true,
  connections: 8,
  level: 0,
  retentionDays: 0,
  optional: false,
  enabled: true,
  username: '',
  password: '',
};

export function UsenetServersSection({
  hue,
  onChanged,
  add,
  adding,
  onAddClose,
}: {
  hue: number;
  onChanged: () => void;
  /** The Add button at the end of the card's heading. */
  add: ReactNode;
  /** The page asked for a new server's window. */
  adding: boolean;
  onAddClose: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [rows, setRows] = useState<UsenetServerRow[] | null>(null);
  const [editing, setEditing] = useState<UsenetServerSave | null>(null);
  const [confirming, setConfirming] = useState<UsenetServerRow | null>(null);
  const [checked, setChecked] = useState<Record<string, Checked>>({});
  const [testing, setTesting] = useState<ReadonlySet<string>>(new Set());

  const load = useCallback(async () => {
    try {
      setRows(await fetchUsenetServers());
    } catch {
      setRows([]);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function save(s: UsenetServerSave, enabled = s.enabled) {
    setRows(await saveUsenetServer({ ...s, enabled }));
    onChanged();
  }

  async function onToggle(r: UsenetServerRow, enabled: boolean) {
    setRows((cur) => cur?.map((x) => (x.id === r.id ? { ...x, enabled } : x)) ?? cur);
    try {
      await save(asSave(r), enabled);
    } catch {
      toast(t('common.loadFailed'), 'fail');
      await load();
    }
  }

  async function onTest(r: UsenetServerRow) {
    setTesting((s) => new Set(s).add(r.id));
    try {
      const res = await testUsenetServer(asSave(r));
      setChecked((c) => ({ ...c, [r.id]: res }));
    } catch {
      toast(t('common.loadFailed'), 'fail');
    } finally {
      setTesting((s) => {
        const next = new Set(s);
        next.delete(r.id);
        return next;
      });
    }
  }

  async function doRemove(r: UsenetServerRow) {
    setConfirming(null);
    try {
      await deleteUsenetServer(r.id);
      toast(t('accounts.removed'), 'info');
      await load();
      onChanged();
    } catch {
      toast(t('common.loadFailed'), 'fail');
    }
  }

  const form = editing ?? (adding ? BLANK : null);

  return (
    <>
      <SectionTitle hint={t('accounts.usenet.hint')} right={add}>
        {t('accounts.usenet.title')}
      </SectionTitle>
      {rows === null ? (
        <p className="text-sm text-carbon-textMuted">{t('common.loading')}</p>
      ) : rows.length === 0 ? (
        <EmptyState
          nested
          icon={<IconGlobe width={26} height={26} />}
          title={t('accounts.usenet.empty')}
          hint={t('accounts.usenet.emptyHint')}
        />
      ) : (
        <AccountTable
          label={t('accounts.usenet.title')}
          rows={rows.map((r) => ({
            key: r.id,
            iconHost: r.host,
            label: r.level > 0 ? `${r.host} · ${t('accounts.usenet.levelShort', { n: r.level })}` : r.host,
            enabled: r.enabled,
            status: <ServerStatus checked={checked[r.id]} busy={testing.has(r.id)} />,
            onToggle: (v) => void onToggle(r, v),
            onEdit: () => setEditing(asSave(r)),
            onRemove: () => setConfirming(r),
            menu: [
              {
                id: 'actions',
                items: [
                  {
                    id: 'test',
                    label: t('accounts.usenet.test'),
                    icon: <IconCheck width={16} height={16} />,
                    onSelect: () => void onTest(r),
                  },
                ],
              },
            ],
          }))}
        />
      )}

      {form && (
        <ServerDialog
          hue={hue}
          initial={form}
          onClose={() => {
            setEditing(null);
            onAddClose();
          }}
          onSave={async (s) => {
            await save(s);
            setChecked((c) => {
              const next = { ...c };
              delete next[s.id];
              return next;
            });
          }}
        />
      )}

      {confirming && (
        <Modal
          title={t('accounts.remove')}
          hue={hue}
          onClose={() => setConfirming(null)}
          footer={
            <>
              <span className="flex-1" />
              <Button kind="secondary" labelled icon={<IconClose />} title={t('common.cancel')} onClick={() => setConfirming(null)} />
              <Button kind="secondary" icon={<IconTrash width={16} height={16} />} onClick={() => void doRemove(confirming)}>
                {t('accounts.remove')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-text">{t('accounts.removeConfirm', { name: confirming.host })}</p>
        </Modal>
      )}
    </>
  );
}

/** asSave is a listed row as a form starts from it: the stored password stays
 *  stored unless somebody types a new one. */
function asSave(r: UsenetServerRow): UsenetServerSave {
  const { hasPassword, ...rest } = r;
  return { ...rest, password: hasPassword ? REDACTED_HEADER : '' };
}

function ServerStatus({ checked, busy }: { checked?: Checked; busy: boolean }) {
  const { t } = useT();
  if (busy) {
    return (
      <span className="inline-flex items-center gap-1.5 text-meta font-medium text-carbon-textMuted">
        <span aria-hidden className="glim-live h-1.5 w-1.5 shrink-0 rounded-[var(--radius-pill)] bg-accent" />
        {t('accounts.refreshing')}
      </span>
    );
  }
  if (!checked) {
    return (
      <span className="inline-flex items-center gap-1.5 text-meta font-medium text-statusNeutral">
        <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusNeutralSolid" />
        {t('accounts.unchecked')}
      </span>
    );
  }
  if (checked.ok) {
    return (
      <span className="inline-flex items-center gap-1.5 text-meta font-medium text-statusOk">
        <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusOkSolid" />
        {t('accounts.ok')}
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 text-meta font-medium text-statusFail">
      <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusFailSolid" />
      {t('accounts.failed')}
      <InfoBubble tip={checked.detail} />
    </span>
  );
}

/** The tile for a server of a provider the catalogue does not know. */
const OTHER = 'other';

/**
 * ServerDialog is the window a server is added in, the way an account is:
 * every well-known provider a tile, and the form of the one picked in the same
 * window with its server, port and encryption filled in. Opened on a stored
 * server, it is that server's form alone.
 */
function ServerDialog({
  hue,
  initial,
  onClose,
  onSave,
}: {
  hue: number;
  initial: UsenetServerSave;
  onClose: () => void;
  onSave: (s: UsenetServerSave) => Promise<void>;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [s, setS] = useState<UsenetServerSave>(initial);
  const [result, setResult] = useState<Checked | null>(null);
  const [saving, setSaving] = useState(false);
  const [picking, setPicking] = useState(!initial.id);
  const [provider, setProvider] = useState<UsenetProvider | null>(null);
  const [query, setQuery] = useState('');
  const set = (patch: Partial<UsenetServerSave>) => {
    setS((cur) => ({ ...cur, ...patch }));
    setResult(null);
  };

  function pick(id: string) {
    const p = USENET_PROVIDERS.find((x) => x.id === id) ?? null;
    setProvider(p);
    setS(p ? { ...BLANK, host: p.host, port: p.port, connections: p.connections ?? BLANK.connections } : BLANK);
    setResult(null);
    setPicking(false);
  }

  // The other server stays in view whatever the search, since it is the way
  // to a provider the list does not have.
  const brands: Brand[] = [
    ...USENET_PROVIDERS.map((p) => ({ id: p.id, name: p.name, host: p.site, sub: p.site })).filter((b) =>
      brandMatches(b, query),
    ),
    {
      id: OTHER,
      name: t('accounts.usenet.other'),
      mark: (
        <span className="glim-house-mark flex h-full w-full items-center justify-center">
          <IconGlobe width={18} height={18} />
        </span>
      ),
      paint: { tile: '#000000', ink: '#ffffff' },
    },
  ];

  // The button says whether the server took the login; a test that could not
  // run at all says why in a toast.
  async function onTest(): Promise<ButtonVerdict> {
    try {
      const answer = await testUsenetServer(s);
      setResult(answer);
      return answer.ok ? 'ok' : 'fail';
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'fail');
      return 'fail';
    }
  }

  async function onSaveClick() {
    setSaving(true);
    try {
      await onSave(s);
      toast(t('accounts.saved'), 'ok');
      onClose();
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'fail');
    } finally {
      setSaving(false);
    }
  }

  const filled = s.host.trim() !== '';
  let title = t('accounts.usenet.addTitle');
  if (picking) title = t('accounts.usenet.pick');
  else if (initial.id) title = t('accounts.usenet.editTitle', { host: initial.host });
  else if (provider) title = t('accounts.addAccountTitle', { service: provider.name });

  const cancel = <Button kind="secondary" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />;
  return (
    <Modal
      title={title}
      hint={initial.id ? undefined : t('accounts.usenet.hint')}
      hue={hue}
      // The picker's window, which the form of the picked provider stays in.
      wide={!initial.id}
      height={picking ? 'capped' : 'content'}
      holdHeight={picking && query !== ''}
      right={picking && <PickerSearch value={query} onChange={setQuery} placeholder={t('search.placeholder')} />}
      onClose={onClose}
      footer={
        picking ? (
          <>
            <span className="flex-1" />
            {cancel}
          </>
        ) : (
          <>
            {/* A new server's form leads back to the tiles it was picked from;
                an edit has no tiles behind it and offers the way out. */}
            {!initial.id && (
              <Button
                kind="secondary"
                labelled
                icon={<IconBack className="rtl:-scale-x-100" />}
                title={t('onboarding.back')}
                onClick={() => {
                  setPicking(true);
                  setQuery('');
                }}
              />
            )}
            <span className="flex-1" />
            {initial.id && cancel}
            <TestButton
              icon={<IconCheck />}
              label={t('accounts.usenet.test')}
              busyLabel={t('accounts.verifying')}
              words={{ ok: t('test.connected'), fail: t('test.notConnected') }}
              disabled={!filled || saving}
              run={onTest}
              resetKey={JSON.stringify(s)}
            />
            <Button icon={<IconSave />} onClick={() => void onSaveClick()} disabled={!filled || saving}>
              {saving ? t('accounts.saving') : t('accounts.save')}
            </Button>
          </>
        )
      }
    >
      {picking ? (
        <BrandPicker brands={brands} none="" onPick={pick} />
      ) : (
        <div className="flex flex-col gap-4">
          <SettingRow control="long" label={t('accounts.usenet.host')} hint={t('accounts.usenet.hostHint')}>
            <TextInput autoComplete="off" value={s.host} onChange={(e) => set({ host: e.target.value })} />
          </SettingRow>
          <SettingRow control="short" label={t('accounts.usenet.port')} hint={t('accounts.usenet.portHint')}>
            <NumberInput value={s.port} min={0} max={65535} onValue={(n) => set({ port: n })} />
          </SettingRow>
          <SettingRow control="short" label={t('accounts.usenet.connections')} hint={t('accounts.usenet.connectionsHint')}>
            <NumberInput value={s.connections} min={1} max={100} onValue={(n) => set({ connections: n })} />
          </SettingRow>
          <ToggleRow
            label={t('accounts.usenet.tls')}
            hint={t('accounts.usenet.tlsHint')}
            checked={s.tls}
            onChange={(tls) => set({ tls, port: s.port === (s.tls ? 563 : 119) ? (tls ? 563 : 119) : s.port })}
          />
          <SettingRow control="long" label={t('accounts.usernameField')} hint={t('accounts.usenet.loginHint')}>
            <TextInput autoComplete="off" value={s.username} onChange={(e) => set({ username: e.target.value })} />
          </SettingRow>
          <SettingRow control="long" label={t('accounts.passwordField')} hint={t('accounts.usenet.loginHint')}>
            <PasswordInput
              autoComplete="new-password"
              value={s.password}
              onChange={(password) => set({ password })}
              showLabel={t('common.showPassword')}
              hideLabel={t('common.hidePassword')}
            />
          </SettingRow>
          <SettingRow control="short" label={t('accounts.usenet.level')} hint={t('accounts.usenet.levelHint')}>
            <NumberInput value={s.level} min={0} max={9} onValue={(n) => set({ level: n })} />
          </SettingRow>
          <SettingRow control="short" label={t('accounts.usenet.retention')} hint={t('accounts.usenet.retentionHint')}>
            <NumberInput value={s.retentionDays} min={0} onValue={(n) => set({ retentionDays: n })} />
          </SettingRow>
          <ToggleRow
            label={t('accounts.usenet.optional')}
            hint={t('accounts.usenet.optionalHint')}
            checked={s.optional}
            onChange={(optional) => set({ optional })}
          />
          {/* Why the server refused, above the buttons until the next test. */}
          {result && !result.ok && (
            <p className="text-xs text-carbon-textSub">{t('accounts.verifyFailed', { detail: result.detail })}</p>
          )}
        </div>
      )}
    </Modal>
  );
}

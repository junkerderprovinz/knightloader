// The accounts page's card for the own Usenet servers. A server is a settings
// row plus a sealed login (internal/api/routes_usenetservers.go), so it is
// edited here through its own routes rather than the catalogue's credential
// dialog: it has a host, a port and limits that no debrid key has.
import { useCallback, useEffect, useState } from 'react';
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
import { IconClose, IconPlus, IconRetry, IconTrash, IconGlobe } from '../lib/icons';
import { AccountTable } from './AccountTable';
import {
  Button,
  EmptyState,
  Field,
  InfoBubble,
  Modal,
  NumberInput,
  PasswordInput,
  SectionTitle,
  TextInput,
  ToggleRow,
} from './ui';

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

export function UsenetServersSection({ hue, onChanged }: { hue: number; onChanged: () => void }) {
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

  const addButton = (
    <Button kind="secondary" hue={hue} icon={<IconPlus width={16} height={16} />} onClick={() => setEditing({ ...BLANK })}>
      {t('accounts.usenet.add')}
    </Button>
  );

  return (
    <>
      <SectionTitle hint={t('accounts.usenet.hint')}>{t('accounts.usenet.title')}</SectionTitle>
      {rows === null ? (
        <p className="text-sm text-carbon-textMuted">{t('common.loading')}</p>
      ) : rows.length === 0 ? (
        <EmptyState
          nested
          icon={<IconGlobe width={26} height={26} />}
          title={t('accounts.usenet.empty')}
          hint={t('accounts.usenet.emptyHint')}
          action={addButton}
        />
      ) : (
        <>
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
                      icon: <IconRetry width={16} height={16} />,
                      onSelect: () => void onTest(r),
                    },
                  ],
                },
              ],
            }))}
          />
          <span className="self-start">{addButton}</span>
        </>
      )}

      {editing && (
        <ServerDialog
          hue={hue}
          initial={editing}
          onClose={() => setEditing(null)}
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
              <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={() => setConfirming(null)} />
              <Button kind="ghost" icon={<IconTrash width={16} height={16} />} onClick={() => void doRemove(confirming)}>
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
      <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-carbon-textMuted">
        <span aria-hidden className="glim-live h-1.5 w-1.5 shrink-0 rounded-[var(--radius-pill)] bg-accent" />
        {t('accounts.refreshing')}
      </span>
    );
  }
  if (!checked) {
    return (
      <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-statusNeutral">
        <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusNeutralSolid" />
        {t('accounts.unchecked')}
      </span>
    );
  }
  if (checked.ok) {
    return (
      <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-statusOk">
        <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusOkSolid" />
        {t('accounts.ok')}
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-statusFail">
      <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusFailSolid" />
      {t('accounts.failed')}
      <InfoBubble tip={checked.detail} />
    </span>
  );
}

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
  const [busy, setBusy] = useState<'test' | 'save' | null>(null);
  const set = (patch: Partial<UsenetServerSave>) => {
    setS((cur) => ({ ...cur, ...patch }));
    setResult(null);
  };

  async function onTest() {
    setBusy('test');
    try {
      setResult(await testUsenetServer(s));
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'fail');
    } finally {
      setBusy(null);
    }
  }

  async function onSaveClick() {
    setBusy('save');
    try {
      await onSave(s);
      toast(t('accounts.saved'), 'ok');
      onClose();
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'fail');
    } finally {
      setBusy(null);
    }
  }

  const filled = s.host.trim() !== '';
  return (
    <Modal
      title={initial.id ? t('accounts.usenet.editTitle', { host: initial.host }) : t('accounts.usenet.addTitle')}
      hue={hue}
      onClose={onClose}
      footer={
        <>
          <span className="flex-1" />
          <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
          <Button kind="secondary" onClick={() => void onTest()} disabled={!filled || busy !== null}>
            {busy === 'test' ? t('accounts.verifying') : t('accounts.usenet.test')}
          </Button>
          <Button onClick={() => void onSaveClick()} disabled={!filled || busy !== null}>
            {busy === 'save' ? t('accounts.saving') : t('accounts.save')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label={t('accounts.usenet.host')} hint={t('accounts.usenet.hostHint')}>
          <TextInput autoComplete="off" value={s.host} onChange={(e) => set({ host: e.target.value })} />
        </Field>
        <div className="grid grid-cols-2 gap-4">
          <Field label={t('accounts.usenet.port')} hint={t('accounts.usenet.portHint')}>
            <NumberInput value={s.port} min={0} max={65535} onValue={(n) => set({ port: n })} />
          </Field>
          <Field label={t('accounts.usenet.connections')} hint={t('accounts.usenet.connectionsHint')}>
            <NumberInput value={s.connections} min={1} max={100} onValue={(n) => set({ connections: n })} />
          </Field>
        </div>
        <ToggleRow
          label={t('accounts.usenet.tls')}
          hint={t('accounts.usenet.tlsHint')}
          checked={s.tls}
          onChange={(tls) => set({ tls, port: s.port === (s.tls ? 563 : 119) ? (tls ? 563 : 119) : s.port })}
        />
        <Field label={t('accounts.usernameField')} hint={t('accounts.usenet.loginHint')}>
          <TextInput autoComplete="off" value={s.username} onChange={(e) => set({ username: e.target.value })} />
        </Field>
        <Field label={t('accounts.passwordField')} hint={t('accounts.usenet.loginHint')}>
          <PasswordInput
            autoComplete="new-password"
            value={s.password}
            onChange={(password) => set({ password })}
            showLabel={t('common.showPassword')}
            hideLabel={t('common.hidePassword')}
          />
        </Field>
        <div className="grid grid-cols-2 gap-4">
          <Field label={t('accounts.usenet.level')} hint={t('accounts.usenet.levelHint')}>
            <NumberInput value={s.level} min={0} max={9} onValue={(n) => set({ level: n })} />
          </Field>
          <Field label={t('accounts.usenet.retention')} hint={t('accounts.usenet.retentionHint')}>
            <NumberInput value={s.retentionDays} min={0} onValue={(n) => set({ retentionDays: n })} />
          </Field>
        </div>
        <ToggleRow
          label={t('accounts.usenet.optional')}
          hint={t('accounts.usenet.optionalHint')}
          checked={s.optional}
          onChange={(optional) => set({ optional })}
        />
        {result && (
          <p className={`text-xs ${result.ok ? 'text-statusOk' : 'text-statusFail'}`} role="status">
            {result.ok ? t('accounts.ok') : t('accounts.verifyFailed', { detail: result.detail })}
          </p>
        )}
      </div>
    </Modal>
  );
}

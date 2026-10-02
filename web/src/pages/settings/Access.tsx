import { useEffect, useState } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import {
  Button,
  Card,
  Field,
  FieldGroup,
  IconBadge,
  IconTile,
  LabelBadge,
  Modal,
  PasswordInput,
  SectionTitle,
  TextInput,
  ToggleRow,
} from '../../components/ui';
import { Tabs } from '../../components/Tabs';
import {
  type ApiToken,
  type AuthState,
  type NewApiToken,
  type TokenScope,
  TOKEN_SCOPES,
  createToken,
  fetchAuth,
  fetchRemoteAccess,
  fetchTokens,
  revokeToken,
  setPassword,
} from '../../lib/api';
import { copyToClipboard } from '../../lib/clipboard';
import { fmtDate } from '../../lib/format';
import { useT } from '../../lib/i18n';
import {
  IconAdd,
  IconCheck,
  IconCheckDrawn,
  IconClose,
  IconCopy,
  IconKey,
  IconTrash,
} from '../../lib/icons';
import { useToast } from '../../lib/toast';
import { ModuleToggle } from './ModuleToggle';
import { PasskeyCard } from './access/PasskeyCard';
import {
  PRESET_LABEL,
  PRESET_SCOPES,
  SCOPE_HINT,
  SCOPE_LABEL,
  presetOf,
  scopesLabel,
  withScope,
  type TokenPreset,
} from './access/tokenScopes';
import { TwoFactorCard } from './access/TwoFactorCard';

export function Access() {
  /**
   * Bumped when anything changes the lock, so the password, second-factor and
   * passkey cards agree: the password enables the other two and removing it
   * takes the second factor with it.
   */
  const [authVersion, setAuthVersion] = useState(0);

  // The pairing section lived on this page, so an old link to it lands
  // on the page it has moved to.
  if (useLocation().hash === '#pairing') return <Navigate to="/settings/pairing" replace />;

  return (
    <div className="flex flex-col gap-10">
      <PasswordCard onAuthChanged={() => setAuthVersion((n) => n + 1)} />

      {/* The second factor and passkeys, two cards because somebody can want
          one without the other. */}
      <SecondWaysIn version={authVersion} onChanged={() => setAuthVersion((n) => n + 1)} />

      <TokensSection />
    </div>
  );
}

// PasswordCard owns the password lock. It saves on its own button and not
// through PUT /api/settings.
function PasswordCard({
  /** Called after the password changes, so the other cards re-read the lock. */
  onAuthChanged,
}: {
  onAuthChanged: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [auth, setAuth] = useState<AuthState | null>(null);
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [done, setDone] = useState(false);
  // The failure counter of the apply button, so a repeated refusal shakes it again.
  const [shake, setShake] = useState(0);
  // Whether this instance is reachable from elsewhere, which turns "no password"
  // into a problem (routes_remote.go's Exposed).
  const [exposed, setExposed] = useState(false);

  useEffect(() => {
    fetchAuth()
      .then(setAuth)
      .catch(() => setAuth(null));
    fetchRemoteAccess()
      .then((info) => setExposed(info.exposed))
      .catch(() => setExposed(false));
  }, []);

  async function onApply() {
    try {
      setAuth(await setPassword(current, next));
      setCurrent('');
      setNext('');
      setDone(true);
      setTimeout(() => setDone(false), 1800);
      onAuthChanged();
    } catch (e) {
      // The reason goes to the toast and the button shakes.
      toast(String(e).replace(/^Error:\s*/, ''), 'fail');
      setShake((n) => n + 1);
    }
  }

  const locked = auth?.enabled ?? false;

  return (
      <Card hue={0} className="flex flex-col gap-5">
        {/* The status stays visible; why a password matters is in the title's hint. */}
        <SectionTitle hint={t('settings.lockHint')}>
          {t('auth.password')}
        </SectionTitle>
        {locked && (
          <Field label={t('settings.passwordCurrent')}>
            <PasswordInput
              value={current}
              onChange={setCurrent}
              autoComplete="current-password"
              showLabel={t('common.showPassword')}
              hideLabel={t('common.hidePassword')}
            />
          </Field>
        )}
        <Field label={t('settings.passwordNew')} hint={t('settings.passwordHint')}>
          <PasswordInput
            value={next}
            onChange={setNext}
            autoComplete="new-password"
            showLabel={t('common.showPassword')}
            hideLabel={t('common.hidePassword')}
          />
        </Field>
        <div className="flex flex-wrap items-center gap-3">
          <Button
            shake={shake}
            kind="secondary"
            hue={0}
            onClick={onApply}
            disabled={locked ? current === '' : next === ''}
          >
            {next === '' && locked ? t('settings.removePassword') : t('settings.setPassword')}
          </Button>
          {/* Beside the button that changes it, in the warning hue only when
              this instance is reachable from elsewhere. */}
          <span
            className={`text-sm ${locked ? 'text-statusOk' : exposed ? 'text-statusWarn' : 'text-carbon-textSub'}`}
          >
            {locked ? t('settings.lockOn') : t('settings.lockOff')}
          </span>
          {done && <span className="text-statusOk text-sm">{t('settings.passwordSaved')}</span>}
        </div>
      </Card>
  );
}

/**
 * SecondWaysIn reads /api/auth once for the second-factor and passkey cards, so
 * they cannot disagree, and renders nothing until it answers, since a guessed
 * "no password" would offer a setup the server refuses.
 */
function SecondWaysIn({ version, onChanged }: { version: number; onChanged: () => void }) {
  const [auth, setAuth] = useState<AuthState | null>(null);

  useEffect(() => {
    fetchAuth()
      .then(setAuth)
      .catch(() => setAuth(null));
  }, [version]);

  if (!auth) return null;
  return (
    <>
      <TwoFactorCard
        hue={4}
        passwordSet={auth.enabled}
        enabled={auth.twoFactor === true}
        recoveryLeft={auth.recoveryLeft}
        onChanged={onChanged}
      />
      <PasskeyCard hue={6} passwordSet={auth.enabled} />
    </>
  );
}

export function TokensSection() {
  const { t } = useT();
  const { toast } = useToast();
  const [tokens, setTokens] = useState<ApiToken[]>([]);
  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState('');
  const [creating, setCreating] = useState(false);
  // The failure counter of the create button, so a repeated refusal shakes it again.
  const [createShake, setCreateShake] = useState(0);
  const [created, setCreated] = useState<NewApiToken | null>(null);
  const [copied, setCopied] = useState(false);
  const [copies, setCopies] = useState(0);
  const [revoking, setRevoking] = useState<string | null>(null);
  // Full access to start with, as for a token created without naming its rights.
  const [scopes, setScopes] = useState<TokenScope[]>([...TOKEN_SCOPES]);

  const load = () => fetchTokens().then(setTokens).catch(() => {});
  useEffect(() => {
    load();
  }, []);

  async function onCreate() {
    setCreating(true);
    try {
      const tok = await createToken(name.trim(), scopes);
      setCreated(tok);
      setName('');
      setScopes([...TOKEN_SCOPES]);
      await load();
    } catch (e) {
      // The window stays open with the typed name; the reason goes to the toast.
      toast(t('settings.access.tokens.createFailed', { error: String(e).replace(/^Error:\s*/, '') }), 'fail');
      setCreateShake((n) => n + 1);
    } finally {
      setCreating(false);
    }
  }

  async function onRevoke(id: string) {
    setRevoking(id);
    try {
      await revokeToken(id);
      await load();
    } finally {
      setRevoking(null);
    }
  }

  function closeCreate() {
    setShowCreate(false);
    setCreated(null);
    setCopied(false);
    setScopes([...TOKEN_SCOPES]);
  }

  const canCreate = !creating && name.trim() !== '' && scopes.length > 0;

  return (
    <>
      <Card hue={5} className="flex flex-col gap-3">
        <SectionTitle hint={t('settings.access.tokens.intro')}>
          {t('settings.access.tokens.title')}
        </SectionTitle>
        <ModuleToggle id="downloadclient" hue={5} />
        {tokens.length === 0 ? (
          <p className="text-sm text-carbon-textMuted">{t('settings.access.tokens.empty')}</p>
        ) : (
          <div className="flex flex-col divide-y divide-carbon-border/40">
            {tokens.map((tok) => (
              <div key={tok.id} className="flex flex-wrap items-center gap-x-3 gap-y-2 py-2.5 first:pt-0 last:pb-0">
                {/* An inert tile marking the row, not a control. */}
                <IconTile icon={<IconKey width={16} height={16} />} hue={5} />
                <div className="min-w-[10rem] flex-1">
                  <div className="truncate text-sm text-carbon-text">{tok.name}</div>
                  <div className="text-[11px] text-carbon-textMuted">
                    {t('settings.access.tokens.created')} {fmtDate(tok.createdAt)}
                    {' · '}
                    {t('settings.access.tokens.lastUsed')}{' '}
                    {tok.lastUsed ? fmtDate(tok.lastUsed) : t('settings.access.tokens.neverUsed')}
                  </div>
                </div>
                {/* The rights and the revoke button go under the name on a
                    narrow screen rather than squeezing it. */}
                <div className="ms-auto flex shrink-0 items-center gap-3">
                  <LabelBadge label={scopesLabel(t, tok.scopes)} hue={5} />
                  {/* `labelled`, so the action follows the Beschriftung setting. */}
                  <IconBadge
                    labelled
                    hue={5}
                    icon={<IconTrash width={16} height={16} />}
                    disabled={revoking === tok.id}
                    title={t('settings.access.tokens.revoke')}
                    aria-label={t('settings.access.tokens.revoke')}
                    onClick={() => void onRevoke(tok.id)}
                    className="shrink-0"
                  />
                </div>
              </div>
            ))}
          </div>
        )}
        {/* Below the list and left-aligned, as "add another". */}
        <div>
          <Button
            kind="secondary"
            hue={5}
            icon={<IconAdd width={16} height={16} />}
            onClick={() => setShowCreate(true)}
          >
            {t('settings.access.tokens.new')}
          </Button>
        </div>
      </Card>

      {showCreate && !created && (
        <Modal
          title={t('settings.access.tokens.new')}
          onClose={() => (creating ? undefined : closeCreate())}
          footer={
            <>
              <span className="flex-1" />
              <Button
                kind="ghost"
                labelled
                icon={<IconClose />}
                title={t('settings.access.tokens.cancel')}
                onClick={closeCreate}
                disabled={creating}
              />
              <Button
                shake={createShake}
                kind="primary"
                onClick={() => void onCreate()}
                disabled={!canCreate}
              >
                {creating ? t('settings.access.tokens.creating') : t('settings.access.tokens.create')}
              </Button>
            </>
          }
        >
          <div className="flex flex-col gap-4">
            <Field label={t('settings.access.tokens.name')}>
              <TextInput
                autoFocus
                placeholder={t('settings.access.tokens.namePlaceholder')}
                value={name}
                onChange={(e) => setName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && canCreate) void onCreate();
                }}
              />
            </Field>
            <TokenScopePicker scopes={scopes} onChange={setScopes} />
          </div>
        </Modal>
      )}

      {created && (
        <Modal
          title={t('settings.access.tokens.secretTitle')}
          hint={t('settings.access.tokens.howToUse')}
          onClose={closeCreate}
          footer={
            <>
              <span className="flex-1" />
              {/* The one way out, and it acknowledges the secret rather than
                  dismissing it, so the glyph is a check. */}
              <Button
                kind="primary"
                labelled
                icon={<IconCheck />}
                title={t('settings.access.tokens.done')}
                onClick={closeCreate}
              />
            </>
          }
        >
          <div className="flex flex-col gap-3">
            <p className="text-sm text-statusFail">{t('settings.access.tokens.secretWarning')}</p>
            <div className="flex items-center gap-2">
              <div className="min-w-0 flex-1 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2">
                <code className="glim-num block overflow-x-auto whitespace-nowrap text-xs text-carbon-text" dir="ltr">
                  {created.secret}
                </code>
              </div>
              {/* `labelled`, so the action follows the Beschriftung setting. */}
              <IconBadge
                labelled
                hue={5}
                icon={copied ? <IconCheckDrawn width={16} height={16} /> : <IconCopy width={16} height={16} />}
                title={copied ? t('settings.access.tokens.copied') : t('settings.access.tokens.copy')}
                aria-label={copied ? t('settings.access.tokens.copied') : t('settings.access.tokens.copy')}
                confirm={copies}
                onClick={async () => {
                  if (await copyToClipboard(created.secret)) {
                    setCopied(true);
                    setCopies((n) => n + 1);
                    setTimeout(() => setCopied(false), 1800);
                  }
                }}
              />
            </div>
          </div>
        </Modal>
      )}
    </>
  );
}

const PRESETS: TokenPreset[] = ['full', 'addRead', 'read', 'custom'];

/**
 * TokenScopePicker chooses what a new token may do: a preset, or the four
 * rights one by one under "custom". The preset is kept apart from the rights,
 * so choosing "custom" opens the switches on the rights already chosen instead
 * of snapping back to the preset they happen to match.
 */
function TokenScopePicker({
  scopes,
  onChange,
}: {
  scopes: readonly TokenScope[];
  onChange: (scopes: TokenScope[]) => void;
}) {
  const { t } = useT();
  const [preset, setPreset] = useState<TokenPreset>(() => presetOf(scopes));

  return (
    <div className="flex flex-col gap-3">
      {/* FieldGroup, because a Field's label would pass a click on the
          caption to the first segment. */}
      <FieldGroup label={t('settings.access.tokens.rights')} hint={t('settings.access.tokens.rightsHint')}>
        <Tabs
          variant="well"
          size="sm"
          label={t('settings.access.tokens.rights')}
          active={preset}
          onSelect={(id) => {
            const next = id as TokenPreset;
            setPreset(next);
            if (next !== 'custom') onChange([...PRESET_SCOPES[next]]);
          }}
          items={PRESETS.map((id) => ({ id, label: t(PRESET_LABEL[id]) }))}
        />
      </FieldGroup>
      {preset === 'custom' && (
        <div className="flex flex-col gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 p-3">
          {TOKEN_SCOPES.map((s, i) => (
            <ToggleRow
              key={s}
              hue={i}
              label={t(SCOPE_LABEL[s])}
              hint={t(SCOPE_HINT[s])}
              checked={scopes.includes(s)}
              onChange={(on) => onChange(withScope(scopes, s, on))}
            />
          ))}
        </div>
      )}
    </div>
  );
}

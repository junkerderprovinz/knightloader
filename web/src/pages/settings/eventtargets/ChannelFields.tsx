import { NumberInput } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { useT } from '../../../lib/i18n';
import {
  balancedTemplate,
  formatHeaders,
  parseHeaders,
  usableAddress,
  type Channel,
  type SmtpTls,
  type WebhookMethod,
} from '../../../lib/eventtargets';
import { SettingRow } from '../controls';
import { CommitText } from './CommitText';

/** templateAddress is an address that may hold placeholders. */
const templateAddress = (v: string) => balancedTemplate(v) && usableAddress(v);

/**
 * ChannelFields is what a channel of its kind needs to send: an address, and a
 * token, a room or a mail server where the service wants one. Each box hands
 * its value on when it loses focus.
 */
export function ChannelFields({ row, onChange }: { row: Channel; onChange: (next: Channel) => void }) {
  const { t } = useT();
  const set = <K extends keyof Channel>(key: K, value: Channel[K]) => onChange({ ...row, [key]: value });

  const url = (hint: string, value: string, commit: (v: string) => void, placeholder: string) => (
    <SettingRow control="long" label={t('settings.eventTargets.url')} hint={hint}>
      <CommitText value={value} onCommit={commit} check={usableAddress} placeholder={placeholder} />
    </SettingRow>
  );
  const token = (hint: string, value: string, commit: (v: string) => void) => (
    <SettingRow control="long" label={t('settings.eventTargets.token')} hint={hint}>
      <CommitText value={value} onCommit={commit} secret />
    </SettingRow>
  );

  switch (row.kind) {
    case 'discord':
      return url(t('settings.eventTargets.discordUrlHint'), row.discord?.url ?? '', (v) => set('discord', { url: v }), 'https://discord.com/api/webhooks/...');
    case 'slack':
      return url(t('settings.eventTargets.slackUrlHint'), row.slack?.url ?? '', (v) => set('slack', { url: v }), 'https://hooks.slack.com/services/...');
    case 'gotify': {
      const g = row.gotify ?? { url: '' };
      return (
        <>
          {url(t('settings.eventTargets.gotifyUrlHint'), g.url, (v) => set('gotify', { ...g, url: v }), 'https://gotify.example')}
          {token(t('settings.eventTargets.gotifyTokenHint'), g.token ?? '', (v) => set('gotify', { ...g, token: v }))}
        </>
      );
    }
    case 'ntfy': {
      const n = row.ntfy ?? { url: '' };
      return (
        <>
          {url(t('settings.eventTargets.ntfyUrlHint'), n.url, (v) => set('ntfy', { ...n, url: v }), 'https://ntfy.sh/my-downloads')}
          {token(t('settings.eventTargets.ntfyTokenHint'), n.token ?? '', (v) => set('ntfy', { ...n, token: v }))}
        </>
      );
    }
    case 'matrix': {
      const m = row.matrix ?? { homeserver: '', room: '', token: '' };
      return (
        <>
          <SettingRow control="long" label={t('settings.eventTargets.homeserver')} hint={t('settings.eventTargets.homeserverHint')}>
            <CommitText value={m.homeserver} onCommit={(v) => set('matrix', { ...m, homeserver: v })} check={usableAddress} placeholder="https://matrix.example" />
          </SettingRow>
          <SettingRow control="long" label={t('settings.eventTargets.room')} hint={t('settings.eventTargets.roomHint')}>
            <CommitText value={m.room} onCommit={(v) => set('matrix', { ...m, room: v })} placeholder="#downloads:matrix.example" />
          </SettingRow>
          {token(t('settings.eventTargets.matrixTokenHint'), m.token, (v) => set('matrix', { ...m, token: v }))}
        </>
      );
    }
    case 'smtp': {
      const s = row.smtp ?? { host: '', from: '', to: '' };
      const put = (next: Partial<typeof s>) => set('smtp', { ...s, ...next });
      return (
        <>
          <SettingRow control="long" label={t('settings.eventTargets.smtpHost')} hint={t('settings.eventTargets.smtpHostHint')}>
            <CommitText value={s.host} onCommit={(v) => put({ host: v })} placeholder="smtp.example.org" />
          </SettingRow>
          <SettingRow label={t('settings.eventTargets.smtpTls')} hint={t('settings.eventTargets.smtpTlsHint')}>
            <Tabs
              variant="well"
              size="sm"
              inline
              hug
              label={t('settings.eventTargets.smtpTls')}
              active={s.tls || 'starttls'}
              onSelect={(id) => put({ tls: id as SmtpTls })}
              items={[
                { id: 'starttls', label: 'STARTTLS' },
                { id: 'tls', label: 'TLS' },
                { id: 'none', label: t('settings.eventTargets.smtpTls.none') },
              ]}
            />
          </SettingRow>
          <SettingRow control="short" label={t('settings.eventTargets.smtpPort')} hint={t('settings.eventTargets.smtpPortHint')}>
            <NumberInput dir="ltr" value={s.port ?? 0} min={0} max={65535} step={1} onValue={(v) => put({ port: Math.max(0, Math.round(v)) || undefined })} />
          </SettingRow>
          <SettingRow control="long" label={t('settings.eventTargets.smtpUser')}>
            <CommitText value={s.username ?? ''} onCommit={(v) => put({ username: v || undefined })} />
          </SettingRow>
          <SettingRow control="long" label={t('settings.eventTargets.smtpPassword')}>
            <CommitText value={s.password ?? ''} onCommit={(v) => put({ password: v || undefined })} secret />
          </SettingRow>
          <SettingRow control="long" label={t('settings.eventTargets.smtpFrom')}>
            <CommitText value={s.from} onCommit={(v) => put({ from: v })} placeholder="knightloader@example.org" />
          </SettingRow>
          <SettingRow control="long" label={t('settings.eventTargets.smtpTo')} hint={t('settings.eventTargets.smtpToHint')}>
            <CommitText value={s.to} onCommit={(v) => put({ to: v })} />
          </SettingRow>
        </>
      );
    }
    case 'apprise': {
      const a = row.apprise ?? { url: '' };
      return (
        <>
          {url(t('settings.eventTargets.appriseUrlHint'), a.url, (v) => set('apprise', { ...a, url: v }), 'http://apprise:8000/notify')}
          <SettingRow control="long" label={t('settings.eventTargets.appriseUrls')} hint={t('settings.eventTargets.appriseUrlsHint')}>
            <CommitText
              rows={3}
              value={(a.urls ?? []).join('\n')}
              placeholder="tgram://..."
              onCommit={(v) => {
                const urls = v.split('\n').map((l) => l.trim()).filter((l) => l !== '');
                set('apprise', { ...a, urls: urls.length > 0 ? urls : undefined });
              }}
            />
          </SettingRow>
          <SettingRow control="long" label={t('settings.eventTargets.appriseTags')} hint={t('settings.eventTargets.appriseTagsHint')}>
            <CommitText value={a.tags ?? ''} onCommit={(v) => set('apprise', { ...a, tags: v || undefined })} />
          </SettingRow>
        </>
      );
    }
    case 'webhook':
      return <WebhookFields row={row} onChange={onChange} />;
    default:
      return null;
  }
}

/** WebhookFields is a request of the operator's own: address, method, headers and body. */
function WebhookFields({ row, onChange }: { row: Channel; onChange: (next: Channel) => void }) {
  const { t } = useT();
  const w = row.webhook ?? { url: '' };
  const put = (next: Partial<typeof w>) => onChange({ ...row, webhook: { ...w, ...next } });
  const method: WebhookMethod = w.method ?? 'POST';

  return (
    <>
      <SettingRow control="long" label={t('settings.eventTargets.url')} hint={t('settings.eventTargets.urlHint')}>
        <CommitText value={w.url} onCommit={(v) => put({ url: v })} check={templateAddress} placeholder="https://ntfy.example/my-topic" />
      </SettingRow>
      <SettingRow label={t('settings.eventTargets.method')} hint={t('settings.eventTargets.methodHint')}>
        <Tabs
          variant="well"
          size="sm"
          inline
          hug
          label={t('settings.eventTargets.method')}
          active={method}
          onSelect={(id) => put({ method: id as WebhookMethod })}
          items={[
            { id: 'POST', label: 'POST' },
            { id: 'PUT', label: 'PUT' },
            { id: 'GET', label: 'GET' },
          ]}
        />
      </SettingRow>
      <SettingRow control="long" label={t('settings.eventTargets.headers')} hint={t('settings.eventTargets.headersHint')}>
        <CommitText
          rows={3}
          value={formatHeaders(w.headers)}
          placeholder={'Authorization: Bearer ...\nContent-Type: text/plain'}
          // A bad line refuses the whole block, since a dropped header is a token that silently stops being sent.
          check={(v) => {
            const parsed = parseHeaders(v);
            return parsed !== null && Object.values(parsed).every(balancedTemplate);
          }}
          onCommit={(v) => {
            const parsed = parseHeaders(v) ?? {};
            put({ headers: Object.keys(parsed).length > 0 ? parsed : undefined });
          }}
        />
      </SettingRow>
      {method !== 'GET' && (
        <SettingRow control="long" label={t('settings.eventTargets.body')} hint={t('settings.eventTargets.bodyHint')}>
          <CommitText rows={4} value={w.body ?? ''} placeholder="%%task.name%%" check={balancedTemplate} onCommit={(v) => put({ body: v || undefined })} />
        </SettingRow>
      )}
    </>
  );
}

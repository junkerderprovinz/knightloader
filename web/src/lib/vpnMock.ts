// The VPN routes as the browser plays them while the instance has none of its
// own: the same answers and refusals, kept in this browser. A connect takes a
// few seconds and then holds an address from the documentation range and, where
// the provider forwards one, a port. What it refuses is worded the way the
// engine refuses it, so the page shows every state:
//
//   a config without [Interface] or a private key that is not one   vpn-config-invalid
//   a login or token with "fail" in it                               vpn-login-refused
//   the location "timeout"                                           vpn-timeout
import { ApiError, type VpnField, type VpnProvider, type VpnState } from './api';
import { VPN_PROVIDERS } from './vpnProviders';

const KEY = 'kl-vpn-mock';
export const CONNECT_MS = 2500;

type Login = Partial<Record<VpnField, string>>;

interface Stored {
  active: string;
  logins: Record<string, Login>;
  /** When the active connect settles, in ms since the epoch. */
  readyAt: number;
  failCode: string;
  exitIp: string;
  port: number;
}

const ERRORS: Record<string, string> = {
  'vpn-config-invalid': 'This is not a WireGuard configuration.',
  'vpn-login-refused': 'The provider refused the login.',
  'vpn-timeout': 'The server did not answer.',
};

// A 44-character base64 key of 32 bytes, as wg genkey writes it.
const WG_KEY = /^[A-Za-z0-9+/]{42}[AEIMQUYcgkosw480]=$/;

let memory: Stored | null = null;

function load(): Stored {
  if (memory) return memory;
  try {
    const raw = localStorage.getItem(KEY);
    if (raw) memory = JSON.parse(raw) as Stored;
  } catch {
    // Without storage the state lives as long as the page.
  }
  memory ??= { active: '', logins: {}, readyAt: 0, failCode: '', exitIp: '', port: 0 };
  return memory;
}

function save(s: Stored) {
  memory = s;
  try {
    localStorage.setItem(KEY, JSON.stringify(s));
  } catch {
    // As in load.
  }
}

function state(s: Stored, now: number): VpnState {
  const providers = VPN_PROVIDERS.map((p): VpnProvider => {
    const base: VpnProvider = {
      id: p.id,
      name: p.name,
      fields: [...p.fields],
      portForwarding: p.portForwarding,
      configured: p.id in s.logins,
      state: 'idle',
    };
    if (p.id !== s.active) return base;
    if (now < s.readyAt) return { ...base, state: 'connecting' };
    if (s.failCode) return { ...base, state: 'failed', error: ERRORS[s.failCode], errorCode: s.failCode };
    return {
      ...base,
      state: 'connected',
      exitIp: s.exitIp,
      ...(p.portForwarding ? { forwardedPort: s.port } : {}),
    };
  });
  return { active: s.active, providers };
}

/** failureOf is the code a connect with this login ends in, '' for none. */
function failureOf(login: Login): string {
  if (login.config !== undefined && !(/\[Interface\]/i.test(login.config) && /PrivateKey\s*=/i.test(login.config))) {
    return 'vpn-config-invalid';
  }
  if (login.privateKey !== undefined && !WG_KEY.test(login.privateKey.trim())) return 'vpn-config-invalid';
  const secret = [login.accountNumber, login.token, login.username, login.password].join(' ');
  if (/fail/i.test(secret)) return 'vpn-login-refused';
  if (login.location?.trim().toLowerCase() === 'timeout') return 'vpn-timeout';
  return '';
}

function connect(s: Stored, id: string, fields: Login, now: number): Stored {
  const provider = VPN_PROVIDERS.find((p) => p.id === id);
  if (!provider) throw new ApiError(`no VPN provider "${id}"`, 'vpn-unknown', undefined, 404);
  const login: Login = { ...s.logins[id] };
  for (const [k, v] of Object.entries(fields) as [VpnField, string][]) {
    if (v.trim() !== '') login[k] = v;
  }
  const missing = provider.fields.find((f) => f !== 'location' && !login[f]?.trim());
  if (missing) {
    const err = new ApiError(`${missing} is missing`, 'vpn-field-missing', { field: missing }, 400);
    err.field = missing;
    throw err;
  }
  return {
    active: id,
    logins: { ...s.logins, [id]: login },
    readyAt: now + CONNECT_MS,
    failCode: failureOf(login),
    exitIp: `203.0.113.${2 + Math.floor(Math.random() * 252)}`,
    port: 49152 + Math.floor(Math.random() * 16383),
  };
}

/** mockVpn answers one call to /api/vpn{path}. */
export async function mockVpn(path: string, init?: RequestInit, now = Date.now()): Promise<VpnState> {
  let s = load();
  const method = init?.method ?? 'GET';
  const connectTo = /^\/([^/]+)\/connect$/.exec(path);
  if (method === 'POST' && connectTo) {
    const body = JSON.parse(String(init?.body ?? '{}')) as { fields?: Login };
    s = connect(s, decodeURIComponent(connectTo[1]), body.fields ?? {}, now);
  } else if (method === 'POST' && path === '/disconnect') {
    s = { ...s, active: '' };
  } else if (method === 'DELETE' && path.startsWith('/')) {
    const id = decodeURIComponent(path.slice(1));
    const logins = { ...s.logins };
    delete logins[id];
    s = { ...s, logins, active: s.active === id ? '' : s.active };
  } else if (method !== 'GET' || path !== '') {
    throw new ApiError(`${method} /api/vpn${path} is not a VPN route`, undefined, undefined, 404);
  }
  save(s);
  return state(s, now);
}

/** resetMockVpn drops what the stand-in holds, for tests. */
export function resetMockVpn() {
  memory = null;
  try {
    localStorage.removeItem(KEY);
  } catch {
    // As in load.
  }
}

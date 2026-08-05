// Human-readable formatting for sizes, speeds and ETAs.

export function fmtBytes(n: number): string {
  if (!n || n < 0) return '—';
  const u = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${u[i]}`;
}

export const fmtSpeed = (n: number): string => (n > 0 ? `${fmtBytes(n)}/s` : '');

export function fmtEta(loaded: number, size: number, speed: number): string {
  if (speed <= 0 || size <= 0 || loaded >= size) return '';
  const secs = Math.round((size - loaded) / speed);
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.round(secs / 60)}m`;
  const h = Math.floor(secs / 3600);
  const m = Math.round((secs % 3600) / 60);
  return `${h}h ${m}m`;
}

export function pct(loaded: number, size: number, done: boolean): number {
  if (size > 0) return Math.min(100, Math.round((loaded / size) * 100));
  return done ? 100 : 0;
}

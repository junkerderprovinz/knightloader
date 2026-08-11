import { useEffect, useState } from 'react';
import { type QueueState, type Settings, fetchQueue, fetchSettings, patchSettings, setQueue } from '../lib/api';
import { useT } from '../lib/i18n';
import { useInstanceScope } from '../lib/instance';
import { Button } from './ui';
import { RATE_UNITS, type RateUnit, fmtRateValue, joinRate, splitRate } from '../lib/format';
import { IconPause, IconPlay } from '../lib/icons';

// Long enough not to hammer a server that is genuinely down, short enough that
// the controls are back before anyone has decided the app is broken.
const RETRY_MS = 5000;

/**
 * QueueBar is the master switch plus the speed limit, sitting where the work is
 * rather than three clicks away in Settings. It rides in the shell bar
 * (app/Layout.tsx), so it is on every page and outlives navigation.
 *
 * The switch is deliberately not "pause everything": halting stops the
 * scheduler from handing out new work and leaves running downloads to finish,
 * because aborting a transfer mid-file throws away bytes nobody asked to lose.
 * The running count beside it is what makes that legible.
 *
 * It takes no `base`. A bar the shell can hand an address to is a bar the shell
 * can hand the WRONG address to, and the wrong one here halts a different
 * machine than the list on screen; the scope comes from lib/instance.tsx, which
 * is the same value the page is reading.
 */
export function QueueBar() {
  const { t } = useT();
  const { instance, base } = useInstanceScope();
  const [queue, setQ] = useState<QueueState | null>(null);
  const [cfg, setCfg] = useState<Settings | null>(null);
  // Held separately from cfg so typing a limit does not fight the field, and
  // held as text so a half-typed "1." survives the keystroke that follows it.
  const [limit, setLimit] = useState('');
  const [unit, setUnit] = useState<RateUnit>('KiB/s');
  // Bumped by a failed load to ask again. This used to be mounted once per
  // visit to the download page, so every visit was a fresh attempt; in the
  // shell it mounts once for the session, and without a retry one dropped
  // request at boot would leave the app with no transport control until
  // somebody thought to reload.
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    // Asked for a peer too. This used to skip, because the bar withheld the
    // controls for a peer anyway - and when the switch was given back, the skip
    // was what kept it invisible: `queue` stayed null and the render bailed out
    // above the switch it was meant to draw. Two halves of one decision in two
    // places, and removing only the visible half left a control that existed and
    // never appeared.
    //
    // /api/queue IS forwarded (internal/api/routes_federation.go), so asking is
    // answered. base already carries the scope.
    let live = true;
    let retry = 0;
    fetchQueue(base)
      .then((q) => {
        if (live) setQ(q);
      })
      .catch(() => {
        // The last known state is kept rather than blanked. A bar that
        // disappears on one dropped request is a bar people stop looking for.
        if (live) retry = window.setTimeout(() => setAttempt((n) => n + 1), RETRY_MS);
      });
    return () => {
      live = false;
      clearTimeout(retry);
    };
  }, [base, instance, attempt]);

  // Loaded once, and deliberately not per scope change: the speed limit is a
  // setting of THIS instance whatever the page is showing, because /api/settings
  // is not forwarded to a peer either.
  useEffect(() => {
    fetchSettings()
      .then((s) => {
        setCfg(s);
        // The unit follows the stored value rather than being remembered
        // separately: somebody who set 5 MiB/s should not come back to
        // "5120" in a KiB field and wonder whether it took.
        const { value, unit } = splitRate(s.speedLimit);
        setLimit(fmtRateValue(value));
        setUnit(unit);
      })
      .catch(() => setCfg(null));
  }, []);

  async function toggle() {
    if (!queue) return;
    setQ(await setQueue({ halted: !queue.halted }, base));
  }

  // The limit is saved when the field is left or Enter is pressed, not on every
  // keystroke: saving per character would send a request for "5", "51", "512".
  async function commit(next: { value?: string; unit?: RateUnit } = {}) {
    if (!cfg) return;
    const raw = next.value ?? limit;
    const u = next.unit ?? unit;
    const bytes = joinRate(Math.max(0, Number(raw.replace(',', '.')) || 0), u);
    // Re-derive the unit from what was actually stored, so typing 2048 KiB/s
    // settles as "2 MiB/s" instead of leaving the field in a form the app would
    // never have chosen itself.
    const settled = splitRate(bytes);
    setLimit(fmtRateValue(settled.value));
    setUnit(settled.unit);
    if (bytes === cfg.speedLimit) return;
    // PATCH, not the whole document: this bar only ever knows about
    // speedLimit, and cfg is a snapshot that can already be behind whatever
    // the Settings page (or another tab) saved since it was fetched - a PUT
    // built from `{...cfg, speedLimit: bytes}` would silently put every one
    // of those other fields back to what this bar last saw. See
    // patchSettings' own doc comment (lib/api.ts).
    setCfg(await patchSettings({ speedLimit: bytes }));
  }

  if (!queue) return null;

  // A peer is in view, and the two controls do NOT land in the same place. The
  // switch follows the scope, because /api/queue is forwarded to a peer; the
  // speed limit cannot, because it lives in /api/settings and settings stay on
  // the machine that configures them.
  //
  // Both used to be withheld, with one sentence explaining why. That was the
  // safe reading of a real trap - a button obeying the peer beside a field
  // obeying this box, with nothing on screen saying so - but it withheld a
  // control the server was perfectly willing to forward, so stopping a peer's
  // queue was impossible from the very bar built to make stopping possible
  // anywhere. The switch stays and the limit goes, with the limit's absence
  // explained rather than silent. The shell's scope tag has already named the
  // peer, so neither line repeats it.
  const peer = Boolean(instance);

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button
        kind={queue.halted ? 'primary' : 'secondary'}
        icon={queue.halted ? <IconPlay width={16} height={16} /> : <IconPause width={16} height={16} />}
        onClick={toggle}
        className="px-2.5 text-xs"
      >
        {queue.halted ? t('queue.start') : t('queue.stop')}
      </Button>

      {queue.halted && (
        <span className="text-statusInfo text-[11px]">
          {t('queue.halted')}
        </span>
      )}

      {/* No spacer between the switch and the limit any more. On the download
          page this bar was the full width of the content and pushed the limit
          to the far edge; in the shell it is one item in a row that also holds
          the scope tag and the widget slot, so a flex-1 here would collapse to
          nothing and the two controls read better as one transport cluster. */}
      {peer && (
        <span className="text-[11px] text-carbon-textMuted">{t('queue.peerLimitLocal')}</span>
      )}

      {!peer && (
      <label className="flex items-center gap-2 text-[11px] text-carbon-textMuted">
        {t('queue.limit')}
        <span className="flex items-center gap-1">
          <input
            type="text"
            inputMode="decimal"
            dir="ltr"
            value={limit}
            placeholder="∞"
            aria-label={t('queue.limit')}
            onChange={(e) => setLimit(e.target.value)}
            onBlur={() => commit()}
            onKeyDown={(e) => {
              if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
            }}
            className="glim-num w-16 rounded-[var(--radius-control)] bg-carbon-surface2 px-2 py-1 text-right text-xs
              text-carbon-text outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
          />
          {/* The number is read in whichever unit is picked — type 5, choose
              MiB/s, get 5 MiB/s. Converting instead would make that impossible,
              because switching the unit would rewrite the number the user just
              typed. After committing, the field settles into the largest unit
              that keeps the number whole, so 2048 KiB/s comes back as 2 MiB/s. */}
          <select
            value={unit}
            aria-label={t('queue.limitUnit')}
            onChange={(e) => {
              const u = e.target.value as RateUnit;
              setUnit(u);
              void commit({ unit: u });
            }}
            className="rounded-[var(--radius-control)] bg-carbon-surface2 px-1.5 py-1 text-xs text-carbon-text
              outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
          >
            {RATE_UNITS.map((u) => (
              <option key={u.label} value={u.label}>
                {u.label}
              </option>
            ))}
          </select>
        </span>
      </label>
      )}
    </div>
  );
}

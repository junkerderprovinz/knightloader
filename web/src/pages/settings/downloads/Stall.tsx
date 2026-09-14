import { useState } from 'react';
import { Card, Field, NumberInput, SectionTitle, ToggleRow } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { useDraft } from '../context';

// The server's own bounds, written out here because nothing serves them:
// GET /api/options carries lists of string choices and no numbers, so these
// four live as Go constants (MinStallTimeout, maxStallTimeout,
// DefaultStallRestarts, maxStallRestarts in internal/settings/settings_stall.go)
// and as these four lines, with nothing to catch a drift between the two. Named
// rather than inlined so a change is one edit in one place, and so the hint text
// that repeats the numbers can be found from here.
const MIN_TIMEOUT = 60;
const MAX_TIMEOUT = 86400;
const MAX_RESTARTS = 20;

// Where the master switch starts a fresh install: five minutes, a UI-side
// opening bid and nothing else. The server's default stays 0 (off) and nothing
// here may change that - this number only decides what the FIRST flick of the
// switch writes, on an install that has never had a stall timeout set.
const FIRST_TIMEOUT = 300;

/**
 * Standing still: the mark for a download that is still "running" at 0 B/s, and
 * the optional restart that follows it.
 *
 * The card takes its hue from the caller because the page decides the order of
 * its cards, and a badge sequence that jumps reads as a bug.
 */
export function StallCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  // There is no separate "stall detection on" field: stallTimeout === 0 IS the
  // off state, server-side. So the switch is derived from the number, and
  // switching off has to remember the number it replaced or ten seconds of
  // second thoughts would cost the user their setting.
  const enabled = cfg.stallTimeout > 0;
  const [lastTimeout, setLastTimeout] = useState(FIRST_TIMEOUT);

  // Deliberately NOT a useState(cfg.stallTimeout) initialiser: the draft can
  // still be empty on the first render, and an initialiser would capture that
  // empty state for good. The value is taken at the moment of switching off,
  // when it is certainly the real one.
  const setEnabled = (on: boolean) => {
    if (on) {
      // Math.max keeps the switch out of 1..59, the range sanitizeStall raises
      // to 60 without saying so. A remembered 30 (typed by hand before the
      // switch was flicked off) would otherwise come back as 60 from a save the
      // user did not make.
      patch({ stallTimeout: Math.max(MIN_TIMEOUT, lastTimeout) });
      return;
    }
    setLastTimeout(cfg.stallTimeout);
    // Only stallTimeout is written. stallRestart and stallMaxRestarts keep
    // their stored values: turning the feature off must not quietly destroy the
    // configuration somebody set up inside it.
    patch({ stallTimeout: 0 });
  };

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{t('settings.stall.title')}</SectionTitle>

      <ToggleRow
        hue={0}
        checked={enabled}
        onChange={setEnabled}
        label={t('settings.stall.enabled')}
        hint={t('settings.stall.enabledHint')}
      />

      {/* ABSENT WHILE THE MARK IS OFF, NEVER DIMMED (GlimStone 1.10.0, and the
          test 1.16.0 settled it with). These three used to be dimmed and inert
          under the argument that a control which vanishes teaches nobody what
          the mode can do; the question that decides it is whether the control
          still DOES anything, and nothing here does. A timeout of 0 IS the off
          state, so the box would be showing a number below its own minimum, and
          the two restart controls are read by nothing while no download is ever
          marked as stalled. What the switch put away comes back untouched when
          it comes back on: setEnabled above deliberately writes only
          stallTimeout, so the restart configuration survives the round trip.

          The dimmed-and-inert wrapper this replaces carried a second fault the
          rule names separately (1.9.0): opacity composites a whole subtree, so
          the three (i) bubbles - the only place that could have said why the
          fields were dim - rendered at 40% along with them. */}
      {enabled && (
      <div className="flex flex-col gap-5">
        {/* min is 60 and not 0, so the stepper cannot walk into 1..59, which
            sanitizeStall silently rewrites to 60 (settings_stall.go). A spinner
            offering 30 would be a control that lies about what saving it did.
            "Off" is the switch above, never a 0 typed in here.

            No clamp in onValue either: Math.max(60, v) would make 600
            untypeable, since the keystroke after "6" would already have been
            rewritten to 60. So a hand-typed 30 does reach the server and does
            come back as 60 - that is what the hint is there to say. */}
        <Field label={t('settings.stall.timeout')} hint={t('settings.stall.timeoutHint')}>
          <NumberInput
            value={cfg.stallTimeout}
            min={MIN_TIMEOUT}
            max={MAX_TIMEOUT}
            step={60}
            onValue={(v) => patch({ stallTimeout: v })}
          />
        </Field>

        {/* Its own switch, not a consequence of the one above: marking costs
            nothing, restarting throws away the bytes the stalled attempt had
            already fetched. Torrents are exempt from it server-side
            (app.stallRestartDueLocked) whatever this says - a server rule, said
            in the hint and deliberately not re-implemented here, where it would
            be a second copy free to disagree with the first. */}
        <ToggleRow
          hue={1}
          checked={cfg.stallRestart}
          onChange={(v) => patch({ stallRestart: v })}
          label={t('settings.stall.restart')}
          hint={t('settings.stall.restartHint')}
        />

        {/* min stays 0 and 0 stays reachable: the server reads 0 as
            DefaultStallRestarts (3), not as unlimited, so raising min to 1 or
            translating 0 into 3 in here would both hide a legal value that
            means something. Above 20 comes back as 20.

            ABSENT, not disabled, while the switch directly above it is off:
            same rule again, one level further in. A restart cap under a restart
            that is not running has no state behind it either, and the switch
            deciding it is the row you are looking at rather than one on another
            page. */}
        {cfg.stallRestart && (
        <Field label={t('settings.stall.maxRestarts')} hint={t('settings.stall.maxRestartsHint')}>
          <NumberInput
            value={cfg.stallMaxRestarts}
            min={0}
            max={MAX_RESTARTS}
            step={1}
            onValue={(v) => patch({ stallMaxRestarts: v })}
          />
        </Field>
        )}
      </div>
      )}
    </Card>
  );
}

import { useEffect, useState, type ReactNode } from 'react';
import { IconCheck, IconClose, IconWarning } from '../lib/icons';
import { Button, type ButtonVerdict } from './ui';

/** The word a test button wears for each answer. */
export interface VerdictWords {
  ok: string;
  fail: string;
  /** For a test that can pass with a finding; one that cannot leaves it out. */
  warn?: string;
}

const VERDICT_GLYPH: Record<ButtonVerdict, ReactNode> = {
  ok: <IconCheck />,
  fail: <IconClose />,
  warn: <IconWarning />,
};

/**
 * TestButton is a button that tests something and shows the answer on itself
 * (GlimStone, "Failure feedback"): green with a check once the test passed,
 * red with a cross and a shake when it failed, its word changing with the
 * colour so the colour is never the only signal. What the answer cannot hold,
 * such as the reason for a failure, is the caller's to show as one line above
 * the button.
 */
export function TestButton({
  label,
  busyLabel,
  icon,
  words,
  run,
  resetKey,
  disabled = false,
  hint,
  className,
}: {
  /** The button's words before a test and after an edit. */
  label: string;
  /** Its words while the test runs; the resting label where there are none. */
  busyLabel?: string;
  icon?: ReactNode;
  words: VerdictWords;
  /** Runs the test and resolves to its answer, or to null where there is none to show. */
  run: () => Promise<ButtonVerdict | null>;
  /** What was tested, in any form: once it changes, the answer is stale and goes. */
  resetKey?: unknown;
  disabled?: boolean;
  hint?: string;
  className?: string;
}) {
  const [busy, setBusy] = useState(false);
  const [verdict, setVerdict] = useState<ButtonVerdict | null>(null);
  // The failure counter, so a repeated failure shakes the button again.
  const [shake, setShake] = useState(0);

  useEffect(() => setVerdict(null), [resetKey]);

  async function onClick() {
    setBusy(true);
    setVerdict(null);
    let answer: ButtonVerdict | null;
    try {
      answer = await run();
    } catch {
      answer = 'fail';
    }
    setVerdict(answer);
    if (answer === 'fail') setShake((n) => n + 1);
    setBusy(false);
  }

  const shown = busy ? null : verdict;
  const word = shown ? (words[shown] ?? words.ok) : busy ? (busyLabel ?? label) : label;
  return (
    <>
      <Button
        kind="secondary"
        className={className}
        icon={shown ? VERDICT_GLYPH[shown] : icon}
        verdict={shown ?? undefined}
        shake={shake}
        disabled={disabled || busy}
        hint={hint}
        onClick={() => void onClick()}
      >
        {word}
      </Button>
      <span role="status" className="sr-only">
        {shown ? word : ''}
      </span>
    </>
  );
}

import { useEffect, useState } from 'react';
import { TextArea, TextInput } from '../../../components/ui';

const HALO = 'shadow-[0_0_0_2px_var(--status-warn-text)]';

/**
 * CommitText is a box that hands its value on when it loses focus, not on every
 * keystroke: a switched-on channel with half an address is refused by the
 * server along with the whole settings save. A value check refuses marks the
 * box and is not handed on.
 */
export function CommitText({
  value,
  onCommit,
  check,
  rows,
  placeholder,
  secret,
}: {
  value: string;
  onCommit: (next: string) => void;
  check?: (next: string) => boolean;
  /** A text area of this many rows instead of one line. */
  rows?: number;
  placeholder?: string;
  /** Typed over rather than read, so the browser does not offer to remember it. */
  secret?: boolean;
}) {
  const [text, setText] = useState(value);
  const [bad, setBad] = useState(false);

  // The save answer replaces the draft, so follow what the server kept.
  useEffect(() => {
    setText(value);
    setBad(false);
  }, [value]);

  const commit = () => {
    const next = rows ? text : text.trim();
    if (next === value) {
      setBad(false);
      return;
    }
    if (check && next !== '' && !check(next)) {
      setBad(true);
      return;
    }
    setBad(false);
    onCommit(next);
  };

  if (rows) {
    return (
      // The halo sits on a wrapper because TextArea's className would replace its own styling.
      <div className={bad ? `rounded-[var(--radius-control)] ${HALO}` : ''}>
        <TextArea
          dir="ltr"
          spellCheck={false}
          rows={rows}
          value={text}
          placeholder={placeholder}
          aria-invalid={bad}
          onChange={(e) => {
            setText(e.target.value);
            setBad(false);
          }}
          onBlur={commit}
        />
      </div>
    );
  }
  return (
    <TextInput
      dir="ltr"
      spellCheck={false}
      autoComplete={secret ? 'off' : undefined}
      value={text}
      placeholder={placeholder}
      aria-invalid={bad}
      className={bad ? HALO : ''}
      onChange={(e) => {
        setText(e.target.value);
        setBad(false);
      }}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          e.currentTarget.blur();
        }
      }}
    />
  );
}

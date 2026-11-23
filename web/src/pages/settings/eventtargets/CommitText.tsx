import { useEffect, useId, useState } from 'react';
import { FieldError, TextArea, TextInput } from '../../../components/ui';

/**
 * CommitText is a box that hands its value on when it loses focus, not on every
 * keystroke: a switched-on channel with half an address is refused by the
 * server along with the whole settings save. A value check refuses keeps its
 * text in the box, marked, with the reason under it, and is not handed on.
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
  /** Why next cannot be taken, or null when it can. */
  check?: (next: string) => string | null;
  /** A text area of this many rows instead of one line. */
  rows?: number;
  placeholder?: string;
  /** Typed over rather than read, so the browser does not offer to remember it. */
  secret?: boolean;
}) {
  const [text, setText] = useState(value);
  const [reason, setReason] = useState<string | null>(null);
  const reasonId = useId();

  // The save answer replaces the draft, so follow what the server kept.
  useEffect(() => {
    setText(value);
    setReason(null);
  }, [value]);

  const commit = () => {
    const next = rows ? text : text.trim();
    if (next === value) {
      setReason(null);
      return;
    }
    const refused = next === '' ? null : (check?.(next) ?? null);
    setReason(refused);
    if (refused === null) onCommit(next);
  };

  const marks = {
    'aria-invalid': reason !== null,
    'aria-describedby': reason !== null ? reasonId : undefined,
  };
  const box = rows ? (
    <TextArea
      dir="ltr"
      spellCheck={false}
      rows={rows}
      value={text}
      placeholder={placeholder}
      {...marks}
      onChange={(e) => {
        setText(e.target.value);
        setReason(null);
      }}
      onBlur={commit}
    />
  ) : (
    <TextInput
      dir="ltr"
      spellCheck={false}
      autoComplete={secret ? 'off' : undefined}
      value={text}
      placeholder={placeholder}
      {...marks}
      onChange={(e) => {
        setText(e.target.value);
        setReason(null);
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
  return (
    <>
      {box}
      {reason !== null && <FieldError id={reasonId}>{reason}</FieldError>}
    </>
  );
}

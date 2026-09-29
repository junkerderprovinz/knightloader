// usePhraseEntry takes the twelve words of another instance. It reads a paste
// with line breaks, list numbers and commas, fills twelve numbered slots as
// the words arrive and names an unknown word with its place before anything
// is sent. Pair stays off until twelve known words are there; whether they
// belong together only the server can say, from the checksum.
import { useState, type ReactNode } from 'react';
import { Button, InfoBubble, TextArea } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { IconClipboard, IconLink } from '../../../lib/icons';
import { PHRASE_WORDS, checkPhrase } from '../../../lib/phraseWords';
import { WordSlots } from './WordSlots';

interface PhraseEntryOptions {
  id: string;
  label: string;
  tip: string;
  /** Where a title already says what goes here, the label is only read out,
   *  not shown. */
  bare?: boolean;
  disabled?: boolean;
  busy: boolean;
  /** Sends the words, one space apart, and resolves to the refusal to show,
   *  or null once paired. */
  onPair: (phrase: string) => Promise<string | null>;
}

/**
 * usePhraseEntry is the word field and its Paste and Pair buttons apart, for
 * a window that puts the buttons in its footer.
 */
export function usePhraseEntry({ id, label, tip, bare = false, disabled = false, busy, onPair }: PhraseEntryOptions): {
  field: ReactNode;
  paste: ReactNode;
  pair: ReactNode;
} {
  const { t } = useT();
  const [text, setText] = useState('');
  const [refusal, setRefusal] = useState<string | null>(null);
  const [shake, setShake] = useState(0);
  const { words, unknown, complete } = checkPhrase(text);

  let problem = refusal;
  if (!problem && unknown.length > 0) {
    problem = t('pairing.errUnknownWord', { position: unknown[0] + 1, word: words[unknown[0]] });
  }
  if (!problem && words.length > PHRASE_WORDS) {
    problem = t('pairing.errWordCount', { count: words.length });
  }

  // The clipboard text goes through the same parser as a paste into the
  // field. A browser that will not hand it over (no secure context, or the
  // permission refused) leaves the field to paste into by hand.
  async function pasteClipboard() {
    try {
      const clip = await navigator.clipboard.readText();
      setText(clip);
      setRefusal(null);
    } catch {
      setRefusal(t('pairing.pasteRefused'));
    }
    document.getElementById(id)?.focus();
  }

  async function pair() {
    const answer = await onPair(words.join(' '));
    if (answer === null) {
      setText('');
      return;
    }
    setRefusal(answer);
    setShake((n) => n + 1);
  }

  const field = (
    <div className={`flex flex-col gap-2 ${disabled ? 'opacity-50' : ''}`} data-testid={id}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        {!bare && (
          <label htmlFor={id} className="inline-flex items-center gap-1.5 text-xs font-semibold text-carbon-textSub">
            {label}
            <InfoBubble tip={tip} />
          </label>
        )}
        <span
          className={`glim-num ms-auto text-xs ${complete ? 'text-statusOk' : 'text-carbon-textMuted'}`}
          aria-live="polite"
        >
          {t('pairing.wordCount', { n: words.length })}
        </span>
      </div>
      <TextArea
        id={id}
        value={text}
        onChange={(e) => {
          setText(e.target.value);
          setRefusal(null);
        }}
        rows={2}
        spellCheck={false}
        autoComplete="off"
        autoCapitalize="none"
        dir="ltr"
        disabled={disabled}
        aria-label={bare ? label : undefined}
        aria-invalid={problem ? true : undefined}
        placeholder={t('pairing.enterPlaceholder')}
      />
      <WordSlots words={words} unknown={unknown} />
      <p role="alert" className="text-sm text-statusFail empty:hidden">
        {problem ?? ''}
      </p>
    </div>
  );
  const pairButton = (
    <Button icon={<IconLink />} shake={shake} onClick={() => void pair()} disabled={disabled || busy || !complete}>
      {t('pairing.join')}
    </Button>
  );
  const pasteButton = (
    <Button kind="secondary" icon={<IconClipboard />} onClick={() => void pasteClipboard()} disabled={disabled || busy}>
      {t('pairing.paste')}
    </Button>
  );
  return { field, paste: pasteButton, pair: pairButton };
}

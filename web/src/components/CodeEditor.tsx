// A CodeMirror 6 editor for scripts, bundled into web/dist like everything
// else. CodeMirror owns the DOM under its container, so it gets a file of its
// own with a single ref as the boundary.
import { useEffect, useRef } from 'react';
import { EditorView, basicSetup } from 'codemirror';
import { Compartment, EditorState } from '@codemirror/state';
import { javascript } from '@codemirror/lang-javascript';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { tags } from '@lezer/highlight';

/**
 * Token colours are CSS variables, so the editor follows the theme without
 * branching. Keywords get weight rather than --accent, which marks activity
 * only.
 */
const highlightStyle = HighlightStyle.define([
  { tag: [tags.keyword, tags.controlKeyword, tags.operatorKeyword, tags.definitionKeyword, tags.moduleKeyword], fontWeight: '600' },
  { tag: [tags.string, tags.special(tags.string)], color: 'var(--status-ok-text)' },
  { tag: [tags.comment, tags.lineComment, tags.blockComment], color: 'var(--carbon-textMuted)', fontStyle: 'italic' },
  { tag: [tags.number, tags.bool, tags.null], color: 'var(--status-info-text)' },
  { tag: [tags.propertyName, tags.attributeName], color: 'var(--carbon-textSub)' },
  { tag: [tags.function(tags.variableName), tags.function(tags.propertyName)], color: 'var(--carbon-text)' },
  { tag: [tags.invalid], color: 'var(--status-fail-text)', textDecoration: 'underline wavy' },
  { tag: [tags.regexp], color: 'var(--status-warn-text)' },
]);

// The editor chrome uses the same tokens as TextInput, so it reads as one of
// the app's own controls.
const chrome = EditorView.theme({
  '&': {
    backgroundColor: 'var(--carbon-surface2)',
    color: 'var(--carbon-text)',
    borderRadius: 'var(--radius-control)',
    fontSize: '13px',
  },
  '&.cm-focused': {
    outline: 'none',
    boxShadow: '0 0 0 2px var(--focus-ring)',
  },
  '.cm-content': {
    fontFamily:
      'ui-monospace, "Cascadia Code", "Cascadia Mono", "SF Mono", Consolas, Menlo, monospace',
    caretColor: 'var(--accent)',
    padding: '10px 0',
  },
  '.cm-gutters': {
    backgroundColor: 'var(--carbon-surface2)',
    color: 'var(--carbon-textMuted)',
    border: 'none',
    borderTopLeftRadius: 'var(--radius-control)',
    borderBottomLeftRadius: 'var(--radius-control)',
  },
  '.cm-activeLine': { backgroundColor: 'var(--carbon-hover)' },
  '.cm-activeLineGutter': { backgroundColor: 'var(--carbon-hover)', color: 'var(--carbon-textSub)' },
  '.cm-selectionBackground, &.cm-focused .cm-selectionBackground': {
    backgroundColor: 'var(--accent-soft) !important',
  },
  '.cm-matchingBracket, .cm-nonmatchingBracket': {
    backgroundColor: 'var(--accent-soft)',
    outline: 'none',
  },
  '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--accent)' },
  '.cm-scroller': { overflow: 'auto' },
  '.cm-placeholder': { color: 'var(--carbon-textMuted)' },
});

const readOnlyCompartment = new Compartment();

export function CodeEditor({
  value,
  onChange,
  readOnly = false,
  minHeight = '220px',
  ariaLabel,
}: {
  value: string;
  onChange: (next: string) => void;
  readOnly?: boolean;
  minHeight?: string;
  ariaLabel?: string;
}) {
  const hostRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  // A ref, so a new callback identity does not rebuild the editor and lose its
  // undo history, cursor and scroll.
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    const view = new EditorView({
      state: EditorState.create({
        doc: value,
        extensions: [
          basicSetup,
          javascript(),
          syntaxHighlighting(highlightStyle),
          chrome,
          EditorView.lineWrapping,
          readOnlyCompartment.of(EditorState.readOnly.of(readOnly)),
          // .cm-content already has role="textbox"; this adds only the name.
          EditorView.contentAttributes.of(ariaLabel ? { 'aria-label': ariaLabel } : {}),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) onChangeRef.current(update.state.doc.toString());
          }),
        ],
      }),
      parent: host,
    });
    viewRef.current = view;

    return () => {
      view.destroy();
      viewRef.current = null;
    };
    // `value` only seeds the editor; the effect below reconciles later changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Applies an external change such as opening another script. The comparison
  // skips the echo of the editor's own keystroke, which would move the cursor.
  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    const current = view.state.doc.toString();
    if (current === value) return;
    view.dispatch({ changes: { from: 0, to: current.length, insert: value } });
  }, [value]);

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    view.dispatch({ effects: readOnlyCompartment.reconfigure(EditorState.readOnly.of(readOnly)) });
  }, [readOnly]);

  // No role here, since .cm-content carries it. Code stays left-to-right.
  return (
    <div
      ref={hostRef}
      dir="ltr"
      style={{ minHeight }}
      className="overflow-hidden rounded-[var(--radius-control)] [&_.cm-editor]:h-full"
    />
  );
}

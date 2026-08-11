// A CodeMirror 6 surface for the script editor (census row "Script Editor:
// %s1" - syntax highlighting, and this wave's share of that row; Auto Format
// and Test Compile are not built here, see Scripts.tsx's own doc comment on
// scope). Vendored via npm and bundled by Vite into web/dist like everything
// else this app ships - the census's own note on this row ("must be
// vendored... so no CDN") is satisfied by construction, not by anything
// special done here: web/embed.go embeds the whole dist/ tree.
//
// Isolated in its own file rather than inlined into Scripts.tsx because
// CodeMirror manages its own DOM under the container it is given - it is not
// a React tree - so the boundary between "React owns this" and "CodeMirror
// owns this" has to be exactly one ref wide, and mixing that into a page
// that also owns list state, drafts and network calls is how the two start
// fighting over the same nodes.
import { useEffect, useRef } from 'react';
import { EditorView, basicSetup } from 'codemirror';
import { Compartment, EditorState } from '@codemirror/state';
import { javascript } from '@codemirror/lang-javascript';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { tags } from '@lezer/highlight';

/**
 * Token colours as CSS custom-property references, not hex values - the
 * whole reason this works in both themes with no dark/light branching here.
 * CodeMirror's HighlightStyle generates an ordinary stylesheet under the
 * hood, and `var(--carbon-text)` is exactly as valid a CSS colour as
 * `#f4f4f4` there; it just keeps resolving against whichever theme is
 * currently in force, the same way every Tailwind utility in the rest of the
 * app does.
 *
 * Deliberately NOT using --accent for keywords. index.css's own doc comment
 * is explicit that gold marks activity only - the active nav item, the
 * primary button, progress - and a script full of `const`/`function`/`if`
 * would turn a whole code block gold, which is the same mistake Rules.tsx's
 * own comment warns about for a column of switches ("a column of gold would
 * claim nine things are happening"). Keywords are told apart by WEIGHT
 * instead, matching index.css's "hierarchy comes from type size and colour
 * step, not from borders" rule applied to type weight instead of size.
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

/**
 * The chrome (background, gutters, selection, cursor, active line, focus
 * ring): everything that is not a token colour. Same tokens the rest of the
 * app's form controls read - TextInput's `bg-carbon-surface2` and its
 * `focus:shadow-[0_0_0_2px_var(--focus-ring)]` - so the editor reads as one
 * more control in the same family rather than an embedded foreign widget.
 */
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
  // Read inside the update listener without making the effect below depend on
  // it - re-running the whole mount/unmount over a changing callback identity
  // would tear down and rebuild the editor (losing undo history, cursor
  // position, scroll) on every keystroke of a parent that does not memoise
  // its handler.
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
          // CodeMirror's own .cm-content already carries role="textbox" and
          // aria-multiline="true" - this only adds the name, never a second
          // role. A wrapper div with its own role="textbox" around that
          // element would nest two textbox roles inside one another, which
          // is the actual accessibility bug, not a defence against one.
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
    // Deliberately empty: `value` seeds the editor once, on mount. Every
    // later change to `value` is reconciled by the effect below, which can
    // tell an external reset (open a different script) apart from the
    // editor's own keystroke (see that effect's own comment) - a `value`
    // dependency here would instead tear the whole view down and rebuild it
    // on every keystroke, in a fight with the code above that exists
    // specifically to avoid that.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Reconciles an external change to `value` - switching which script is open,
  // or a Discard - into the live document. Compared against the view's own
  // current text first, or this fires right back after the update listener
  // above reports the very keystroke that produced this `value` in the first
  // place, moving the cursor to the end of the document on every character
  // typed.
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

  // No role or aria-* here: the name and the textbox role both live on
  // CodeMirror's own .cm-content, set via EditorView.contentAttributes above
  // - see that extension's comment for why duplicating either here would be
  // the bug, not the fix. dir="ltr" is the one thing this wrapper is
  // responsible for: code is left-to-right regardless of interface language,
  // the same rule TextInput call sites apply to a URL or a file path.
  return (
    <div
      ref={hostRef}
      dir="ltr"
      style={{ minHeight }}
      className="overflow-hidden rounded-[var(--radius-control)] [&_.cm-editor]:h-full"
    />
  );
}

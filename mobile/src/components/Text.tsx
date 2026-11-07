import { createContext, useContext, useState } from 'react';
import {
  StyleSheet,
  Text as NativeText,
  TextInput as NativeTextInput,
  type StyleProp,
  type TextInputProps,
  type TextProps,
  type TextStyle,
} from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';
import { familyFor } from '../theme/font';
import type { Palette } from '../theme/tokens';

/**
 * Whether the house font has loaded. App provides it once expo-font settles;
 * until then, and for good if loading failed, text keeps the system font and
 * its own fontWeight, which is better than a family nobody registered.
 */
export const HouseFontReady = createContext(false);

/** True inside a Text, so a nested run knows it inherits a cut. */
const InsideText = createContext(false);

/**
 * The style with its fontWeight swapped for the Noto Sans cut of that weight.
 *
 * A nested run without a weight of its own keeps whatever cut it inherits,
 * since naming the regular one would undo a bold parent. An explicit family,
 * such as the monospace of an address, is left alone.
 *
 * includeFontPadding goes off because Noto's glyph box reaches far below its
 * descender to fit Devanagari's stacked marks. Android pads a line to that box
 * by default, which sets every label high in its control; without the padding
 * the line is Noto's own ascent and descent, about the height the system font
 * had with it.
 */
function houseFont(style: StyleProp<TextStyle>, nested: boolean): StyleProp<TextStyle> {
  const flat = StyleSheet.flatten(style) ?? {};
  if (flat.fontFamily || (nested && flat.fontWeight === undefined)) return style;
  const { fontWeight, ...rest } = flat;
  return { includeFontPadding: false, ...rest, fontFamily: familyFor(fontWeight) };
}

/** React Native's Text, set in the house font. Every screen imports this one. */
export function Text(props: TextProps) {
  const ready = useContext(HouseFontReady);
  const nested = useContext(InsideText);
  const text = <NativeText {...props} style={ready ? houseFont(props.style, nested) : props.style} />;
  return nested ? text : <InsideText value>{text}</InsideText>;
}

/**
 * React Native's TextInput, set in the house font, placeholder included.
 *
 * Focus moves the field's fill one step up the ramp rather than drawing a ring
 * (GlimStone's rule 14): the caret already says where the typing goes, and a
 * ring is a line this language does not draw.
 */
export function TextInput(props: TextInputProps) {
  const ready = useContext(HouseFontReady);
  const { c } = useAppearance();
  const [focused, setFocused] = useState(false);
  const style = ready ? houseFont(props.style, false) : props.style;
  const raised = focused ? stepUp(c, StyleSheet.flatten(props.style)?.backgroundColor) : undefined;
  return (
    <NativeTextInput
      {...props}
      style={raised ? [style, { backgroundColor: raised }] : style}
      onFocus={(e) => {
        setFocused(true);
        props.onFocus?.(e);
      }}
      onBlur={(e) => {
        setFocused(false);
        props.onBlur?.(e);
      }}
    />
  );
}

/** The next surface up from the one a field stands on, if it stands on one. */
function stepUp(c: Palette, ground: unknown): string | undefined {
  if (ground === c.surface) return c.surface2;
  if (ground === c.surface2) return c.surface3;
  if (ground === c.surface3) return c.hoverRaised;
  return undefined;
}

import { createContext, useContext } from 'react';
import {
  StyleSheet,
  Text as NativeText,
  TextInput as NativeTextInput,
  type StyleProp,
  type TextInputProps,
  type TextProps,
  type TextStyle,
} from 'react-native';
import { familyFor } from '../theme/font';

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

/** React Native's TextInput, set in the house font, placeholder included. */
export function TextInput(props: TextInputProps) {
  const ready = useContext(HouseFontReady);
  return <NativeTextInput {...props} style={ready ? houseFont(props.style, false) : props.style} />;
}

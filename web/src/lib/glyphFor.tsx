// glyphFor gives GlimStone's own components (the fortress on the Pairing page)
// KnightLoader's drawing for each label key they name, so a verb wears the same
// shape there as on KnightLoader's own buttons.
import type { ReactNode } from 'react';
import {
  IconCheck,
  IconClose,
  IconCopy,
  IconExternalLink,
  IconEye,
  IconInfo,
  IconKey,
  IconLink,
  IconPhone,
  IconRefresh,
  IconServer,
  IconTrash,
  IconUnlink,
} from './icons';

const GLYPHS: Record<string, () => ReactNode> = {
  fortressJoin: () => <IconLink />,
  fortressLeave: () => <IconUnlink />,
  fortressShowPhrase: () => <IconEye />,
  fortressChangePhrase: () => <IconRefresh />,
  unlock: () => <IconKey />,
  copy: () => <IconCopy />,
  confirm: () => <IconCheck />,
  'common.cancel': () => <IconClose />,
  'common.close': () => <IconClose />,
  delete: () => <IconTrash />,
  open: () => <IconExternalLink />,
  info: () => <IconInfo />,
  deviceServer: () => <IconServer />,
  devicePhone: () => <IconPhone />,
};

export function glyphFor(labelKey: string): ReactNode | undefined {
  return GLYPHS[labelKey]?.();
}

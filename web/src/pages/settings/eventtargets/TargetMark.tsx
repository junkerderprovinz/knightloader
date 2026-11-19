import { ProviderMark } from '../../../components/CatalogTiles';
import { catalogEntry } from '../../../lib/catalogs/catalog';
import { NOTIFY_PROVIDERS } from '../../../lib/catalogs/notifyProviders';
import { IconLink } from '../../../lib/icons';

/** The services an event target can be picked as, which word its address and wear their mark. */
export type TargetBrand = 'discord';

/** TargetMark is the mark a service's tile wears in GlimStone's notification catalog. */
export function TargetMark({ brand, size }: { brand: TargetBrand; size: number }) {
  return <ProviderMark mark={catalogEntry(NOTIFY_PROVIDERS, brand)?.mark} glyph={<IconLink />} size={size} />;
}

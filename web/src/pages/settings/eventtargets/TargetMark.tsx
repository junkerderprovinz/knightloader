import { Logo } from '../../../components/HosterIcon';
import { withBase } from '../../../lib/basePath';
import { TARGET_BRANDS, type TargetBrand } from '../../../lib/targetBrands';

const LOGOS = new Map(TARGET_BRANDS.map((b) => [b.id, b.logo]));

/** TargetMark is a service's logo from public/targets; `lit` adds the mask its tile shows under the pointer. */
export function TargetMark({ brand, size, lit }: { brand: TargetBrand; size: number; lit?: boolean }) {
  const logo = LOGOS.get(brand)!;
  const src = withBase(`/targets/${brand}.png`);
  const mask = logo.lit ? withBase(`/targets/${brand}.lit.png`) : src;
  return <Logo src={src} mask={mask} info={logo} box={{ width: size, height: size }} lit={lit} />;
}

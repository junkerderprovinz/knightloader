// The Aussehen tab is a section of Look, so it shares the General tab's draft,
// palette and save error instead of keeping copies of its own.
import { Look } from './Look';

export function Appearance() {
  return <Look section="appearance" />;
}

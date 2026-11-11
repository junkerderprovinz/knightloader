// MovedCard stands where the group's phrase was once this instance is in a
// fortress: its instances, phones and browser extensions pair with the
// fortress phrase, and the ones still on the old phrase follow by themselves.
import { listOf } from '@glimstone/core';
import { Card, SectionTitle } from '../../../components/ui';
import { useT } from '../../../lib/i18n';

export function MovedCard({ left, hue }: { left: string[]; hue?: number }) {
  const { t, lang } = useT();
  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle>{t('pairing.movedTitle')}</SectionTitle>
      <div className="flex flex-col gap-2">
        <p className="text-sm text-carbon-text">{t('pairing.moved')}</p>
        {left.length > 0 && (
          <p className="text-sm text-carbon-textSub">{t('pairing.oldPhrase', { names: listOf(lang, left) })}</p>
        )}
      </div>
    </Card>
  );
}

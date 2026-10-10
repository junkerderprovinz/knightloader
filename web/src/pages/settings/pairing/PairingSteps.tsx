// PairingSteps opens the tab with three numbered cards before any button does
// anything: generate a phrase on the first instance, enter it on every other,
// done. Their pictures play one scene, with one pointer that walks across all
// three.
import { Fragment, useRef, type CSSProperties, type ReactNode } from 'react';
import { SectionTitle } from '../../../components/ui';
import { hueVars } from '../../../lib/appearance';
import { useT } from '../../../lib/i18n';
import { ScenePointer, StepPicture, useStepScene, type StepLabels } from './pairingArt';

/** emphasize puts a bold copy of what where text says {token}. */
export function emphasize(text: string, token: string, what: string): ReactNode {
  const parts = text.split(`{${token}}`);
  return parts.map((part, i) => (
    <Fragment key={i}>
      {part}
      {i < parts.length - 1 && <strong className="font-medium text-carbon-text">{what}</strong>}
    </Fragment>
  ));
}

export function PairingSteps({ hues }: { hues: [number, number, number] }) {
  const { t, lang } = useT();
  const grid = useRef<HTMLDivElement>(null);
  const pointer = useRef<HTMLDivElement>(null);
  const calm = useStepScene(grid, pointer, lang);
  const labels: StepLabels = {
    create: t('pairing.create'),
    enter: t('pairing.enter'),
    copy: t('common.copy'),
    copied: t('common.copied'),
    paste: t('pairing.paste'),
    words: t('pairing.wordsLabel'),
    paired: t('pairing.paired'),
  };
  const steps = [
    { title: t('pairing.step1Title'), body: t('pairing.step1Body') },
    { title: t('pairing.step2Title'), body: emphasize(t('pairing.step2Body'), 'button', t('pairing.enter')) },
    { title: t('pairing.step3Title'), body: t('pairing.step3Body') },
  ];
  return (
    <section aria-label={t('pairing.howTitle')}>
      {/* The badges straddle the cards' top edge, so a card under another needs
          more room above it than one beside it. */}
      <div ref={grid} className="relative grid grid-cols-1 gap-x-5 gap-y-7 min-[861px]:grid-cols-3">
        {steps.map((s, i) => (
          <div
            key={i}
            className="glim-hue relative rounded-[var(--radius-card)] bg-carbon-surface2 px-4 pb-4 pt-7"
            style={hueVars(hues[i]) as CSSProperties}
          >
            <SectionTitle hue={hues[i]} beside={[{ key: 'title', label: s.title }]}>
              <span className="glim-num tracking-normal">{i + 1}</span>
            </SectionTitle>
            <div className="flex flex-col gap-3">
              <StepPicture step={(i + 1) as 1 | 2 | 3} labels={labels} />
              <p className="text-sm text-carbon-textMuted">{s.body}</p>
            </div>
          </div>
        ))}
        {!calm && (
          <div
            ref={pointer}
            aria-hidden="true"
            className="pointer-events-none absolute left-0 top-0 z-[2] origin-top-left opacity-0 [filter:drop-shadow(0_2px_3px_rgb(0_0_0/.35))]"
          >
            <ScenePointer />
          </div>
        )}
      </div>
    </section>
  );
}

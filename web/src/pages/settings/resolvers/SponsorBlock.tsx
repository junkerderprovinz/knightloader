import { Tabs } from '../../../components/Tabs';
import {
  SPONSORBLOCK_CATEGORIES,
  SPONSORBLOCK_MARK_ONLY,
  type SponsorBlockCategory,
  type YtdlpSponsorBlock,
} from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { SettingRow } from '../controls';

type Choice = 'keep' | 'mark' | 'remove';

const CATEGORY_KEYS: Record<SponsorBlockCategory, TranslationKey> = {
  sponsor: 'settings.resolvers.sponsorBlock.sponsor',
  intro: 'settings.resolvers.sponsorBlock.intro',
  outro: 'settings.resolvers.sponsorBlock.outro',
  selfpromo: 'settings.resolvers.sponsorBlock.selfpromo',
  preview: 'settings.resolvers.sponsorBlock.preview',
  filler: 'settings.resolvers.sponsorBlock.filler',
  interaction: 'settings.resolvers.sponsorBlock.interaction',
  music_offtopic: 'settings.resolvers.sponsorBlock.musicOfftopic',
  hook: 'settings.resolvers.sponsorBlock.hook',
  poi_highlight: 'settings.resolvers.sponsorBlock.poiHighlight',
  chapter: 'settings.resolvers.sponsorBlock.chapter',
};

const CHOICE_KEYS: Record<Choice, TranslationKey> = {
  keep: 'settings.resolvers.sponsorBlock.keep',
  mark: 'settings.resolvers.sponsorBlock.mark',
  remove: 'settings.resolvers.sponsorBlock.remove',
};

export const NO_SPONSORBLOCK: YtdlpSponsorBlock = { remove: [], mark: [] };

function choiceOf(sb: YtdlpSponsorBlock, cat: SponsorBlockCategory): Choice {
  if (sb.remove?.includes(cat)) return 'remove';
  return sb.mark?.includes(cat) ? 'mark' : 'keep';
}

/** withChoice moves one category between the two lists, keeping them in offer order so a round trip stores the same lists. */
export function withChoice(sb: YtdlpSponsorBlock, cat: SponsorBlockCategory, choice: Choice): YtdlpSponsorBlock {
  const chosen = (list: Choice) =>
    SPONSORBLOCK_CATEGORIES.filter((c) => (c === cat ? choice === list : choiceOf(sb, c) === list));
  return { remove: chosen('remove'), mark: chosen('mark') };
}

/**
 * SponsorBlockRows offers every SponsorBlock category with what happens to its
 * segments. Cutting is withheld where yt-dlp can only mark, since a highlight
 * or a chapter is a point in the video rather than a stretch of it.
 */
export function SponsorBlockRows({
  value,
  onChange,
}: {
  value: YtdlpSponsorBlock;
  onChange: (next: YtdlpSponsorBlock) => void;
}) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-3">
      {SPONSORBLOCK_CATEGORIES.map((cat) => {
        const label = t(CATEGORY_KEYS[cat]);
        const choices: Choice[] = SPONSORBLOCK_MARK_ONLY.has(cat) ? ['keep', 'mark'] : ['keep', 'mark', 'remove'];
        return (
          <SettingRow key={cat} label={label}>
            <Tabs
              variant="well"
              size="sm"
              inline
              hug
              label={label}
              active={choiceOf(value, cat)}
              onSelect={(id) => onChange(withChoice(value, cat, id as Choice))}
              items={choices.map((c) => ({ id: c, label: t(CHOICE_KEYS[c]) }))}
            />
          </SettingRow>
        );
      })}
    </div>
  );
}

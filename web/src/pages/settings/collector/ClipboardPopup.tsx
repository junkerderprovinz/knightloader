import { Dropdown } from '../../../components/Dropdown';
import { Tabs } from '../../../components/Tabs';
import { ToggleRow } from '../../../components/ui';
import { POPUP_PLACES, POPUP_SECONDS, type PopupSeconds } from '../../../lib/clipboardWatch';
import { useT } from '../../../lib/i18n';
import {
  useClipboardPopup,
  useClipboardPopupPlace,
  useClipboardPopupSeconds,
} from '../../../lib/useClipboardWatch';
import { SettingRow } from '../controls';

/**
 * ClipboardPopupRows set up the desktop app's popup for links the clipboard
 * watch caught: whether it shows, and while it does, how long it stays and
 * where. The switch belongs to the watch, so it takes the watch's colour.
 */
export function ClipboardPopupRows() {
  const { t, lang } = useT();
  const [popup, setPopup] = useClipboardPopup();
  const [seconds, setSeconds] = useClipboardPopupSeconds();
  const [place, setPlace] = useClipboardPopupPlace();
  const inSeconds = new Intl.NumberFormat(lang, { style: 'unit', unit: 'second', unitDisplay: 'short' });

  return (
    <>
      <ToggleRow
        hue={1}
        label={t('intake.clipboardPopup')}
        hint={t('intake.clipboardPopupHint')}
        checked={popup}
        onChange={setPopup}
      />
      {popup && (
        <>
          <SettingRow label={t('intake.clipboardPopupTime')} hint={t('intake.clipboardPopupTimeHint')}>
            <Tabs
              select="one"
              variant="well"
              size="sm"
              inline
              hug
              label={t('intake.clipboardPopupTime')}
              active={String(seconds)}
              onSelect={(id) => setSeconds(Number(id) as PopupSeconds)}
              items={POPUP_SECONDS.map((n) => ({
                id: String(n),
                label: n === 0 ? t('intake.clipboardPopupUntilClicked') : inSeconds.format(n),
              }))}
            />
          </SettingRow>
          <SettingRow control="long" label={t('intake.clipboardPopupPlace')} hint={t('intake.clipboardPopupPlaceHint')}>
            <Dropdown
              label={t('intake.clipboardPopupPlace')}
              value={place}
              onChange={setPlace}
              options={POPUP_PLACES.map((p) => ({ value: p, label: t(`intake.clipboardPopupPlace.${p}`) }))}
            />
          </SettingRow>
        </>
      )}
    </>
  );
}

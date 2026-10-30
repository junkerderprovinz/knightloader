import type { ReactNode } from 'react';
import { useDialogMute, type DialogId } from '../lib/dialogmute';
import { middleEllipsis } from '../lib/format';
import { useT } from '../lib/i18n';
import { IconClose } from '../lib/icons';
import { Tip } from './columns';
import { Button, Checkbox, InfoBubble } from './ui';

/**
 * Notice is a short list that wants a look but no answer, such as links that
 * were not added. Its title carries the count, the rows follow, and its own
 * actions stand in one row at its foot, after the box that switches this kind
 * of notice off; Settings, Appearance brings it back. Every notice in the app
 * is drawn by this, so they all read alike.
 */
export function Notice({
  id,
  title,
  info,
  actions,
  onClose,
  className = '',
  children,
}: {
  id: DialogId;
  title: string;
  info?: string;
  /** The notice's own buttons, in the foot, the forward one last. */
  actions?: ReactNode;
  /** Present where the notice can be put away until something new arrives. */
  onClose?: () => void;
  className?: string;
  children: ReactNode;
}) {
  const { t } = useT();
  const dialogs = useDialogMute();
  if (dialogs.isMuted(id)) return null;
  return (
    <section aria-label={title} className={`glim-card flex flex-col gap-3 p-4 ${className}`}>
      <header className="flex items-center gap-2">
        <h3 className="glim-num flex min-w-0 items-center text-sm font-medium text-carbon-text">
          {title}
          {info && <InfoBubble tip={info} />}
        </h3>
        <span className="flex-1" />
        {onClose && (
          <Button kind="secondary" labelled icon={<IconClose />} title={t('common.dismiss')} onClick={onClose} />
        )}
      </header>
      <div className="flex max-h-56 flex-col overflow-y-auto">{children}</div>
      <footer className="flex flex-wrap items-center justify-end gap-2">
        <Checkbox
          className="me-auto"
          label={t('notice.dontShowAgain')}
          checked={false}
          onChange={(off) => dialogs.setMuted(id, off)}
        />
        {actions}
      </footer>
    </section>
  );
}

/**
 * NoticeRow is one entry of a Notice: what happened, to which link, where it
 * came from and when, then its own action. The columns are fixed, so the rows
 * line up; a long name is shortened in the middle and whole in its bubble.
 */
export function NoticeRow({
  reason,
  name,
  source,
  at,
  action,
}: {
  reason: string;
  name: string;
  source?: ReactNode;
  at: string;
  action?: ReactNode;
}) {
  return (
    <div
      className="glim-row grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-0.5 rounded-[var(--radius-control)] px-1 py-1
        text-xs sm:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)_7rem_7.5rem_auto]"
    >
      <Tip tip={reason} className="truncate text-carbon-text">
        {reason}
      </Tip>
      <Tip tip={name} dir="ltr" className="truncate text-carbon-textMuted max-sm:order-last max-sm:col-span-2">
        {middleEllipsis(name)}
      </Tip>
      <span className="hidden truncate text-carbon-textSub sm:inline">{source}</span>
      <span className="glim-num hidden truncate text-carbon-textMuted sm:inline">{at}</span>
      <span className="justify-self-end">{action}</span>
    </div>
  );
}

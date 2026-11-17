import { useEffect } from 'react';
import { useT } from '../lib/i18n';
import { IconConfirm } from '../lib/icons';
import { Card } from '../components/ui';

/**
 * SignedIn is what the tab a site's sign-in was opened in turns into once
 * KnightLoader has the session (SignInBrowserWindow).
 */
export function SignedIn() {
  const { t } = useT();
  const site = new URLSearchParams(window.location.search).get('site') ?? '';
  const title = site ? t('signedIn.title', { site }) : t('signedIn.titleBare');
  useEffect(() => {
    document.title = title;
  }, [title]);
  return (
    <div className="flex min-h-dvh items-center justify-center bg-carbon-background p-6">
      <Card className="flex w-full max-w-sm flex-col items-center gap-3 text-center">
        <IconConfirm width={32} height={32} className="text-statusOk" />
        <h1 className="text-title font-semibold text-carbon-text" dir="auto">
          {title}
        </h1>
        <p className="text-sm text-carbon-textSub" dir="auto">
          {t('signedIn.body')}
        </p>
      </Card>
    </div>
  );
}

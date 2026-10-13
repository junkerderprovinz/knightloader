import { Component, type ReactNode } from 'react';
import { ErrorCard } from '../components/ui';
import { useT } from '../lib/i18n';

/**
 * PageBoundary catches an error thrown while a page draws, so one broken
 * picture leaves a card with a way back instead of an empty window. React
 * writes the error to the console itself.
 */
export class PageBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  render() {
    return this.state.failed ? <Crashed /> : this.props.children;
  }
}

function Crashed() {
  const { t } = useT();
  return <ErrorCard message={t('app.crashed')} retry={() => window.location.reload()} retryLabel={t('app.reload')} />;
}

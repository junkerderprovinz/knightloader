import { createContext, useContext, useCallback, useRef, useState, type ReactNode } from 'react';

export type ToastTone = 'ok' | 'fail' | 'info';
interface Toast {
  id: number;
  message: string;
  tone: ToastTone;
}

interface ToastAPI {
  toast: (message: string, tone?: ToastTone) => void;
}

const Ctx = createContext<ToastAPI>({ toast: () => {} });

export const useToast = () => useContext(Ctx);

const toneClass: Record<ToastTone, string> = {
  ok: 'text-statusOk',
  fail: 'text-statusFail',
  info: 'text-statusInfo',
};
const dot: Record<ToastTone, string> = {
  ok: 'bg-statusOkSolid',
  fail: 'bg-statusFailSolid',
  info: 'bg-statusInfoSolid',
};

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<Toast[]>([]);
  const seq = useRef(0);

  const toast = useCallback((message: string, tone: ToastTone = 'info') => {
    const id = ++seq.current;
    setItems((s) => [...s, { id, message, tone }]);
    setTimeout(() => setItems((s) => s.filter((t) => t.id !== id)), 4000);
  }, []);

  return (
    <Ctx.Provider value={{ toast }}>
      {children}
      <div className="fixed bottom-5 right-5 z-50 flex flex-col gap-2 pointer-events-none">
        {items.map((t) => (
          <div
            key={t.id}
            role="status"
            className="kl-toast flex items-center gap-2.5 rounded-lg bg-carbon-surface px-4 py-2.5 text-sm text-carbon-text shadow-[var(--elevation)]"
          >
            <span className={`h-2 w-2 shrink-0 rounded-full ${dot[t.tone]}`} />
            <span className={toneClass[t.tone]}>{t.message}</span>
          </div>
        ))}
      </div>
    </Ctx.Provider>
  );
}

// Root-композиция фичи: bridge (SSE → store) + UI (store → modal).
//
// Размещение в feature, не в `src/app/` — по FSD: app только композирует
// провайдеры, доменная логика живёт в feature. App импортит этот компонент
// и монтирует один раз внутри QueryClientProvider/Toaster.
//
// Provider — single-mount компонент: bridge регистрирует ОДНУ глобальную SSE-
// подписку через `useEventStream(undefined)`. Двойной монтаж приведёт к двум
// `EventSource` (мелкая утечка), поэтому держим Provider только в App-shell.
import { useCurrentTypeConfirmation, useLowConfidenceStore } from '../model/low-confidence-store';
import { useConfirmType } from '../model/use-confirm-type';
import { useLowConfidenceBridge } from '../model/use-low-confidence-bridge';
import { LowConfidenceConfirmModal } from './LowConfidenceConfirmModal';

export function LowConfidenceConfirmProvider(): JSX.Element | null {
  useLowConfidenceBridge();
  const event = useCurrentTypeConfirmation();
  const dismiss = useLowConfidenceStore((s) => s.dismiss);
  // useConfirmType вызывается всегда (Rules of Hooks), даже когда модалка
  // не рендерится — стоимость минимальна (один useMutation на App-shell).
  const confirm = useConfirmType();

  if (!event) return null;
  return (
    <LowConfidenceConfirmModal
      event={event}
      onDismiss={dismiss}
      confirm={{ confirm: confirm.confirm, isPending: confirm.isPending }}
    />
  );
}

// Бридж: SSE event `type_confirmation_required` → low-confidence store.
//
// RBAC: для BUSINESS_USER подписка НЕ открывается совсем (`enabled: false`),
// чтобы не держать лишний EventSource — модалку им показывать всё равно
// нечего, и event-bus оркестратора публикует событие на org_id, а не на
// user_id, поэтому BUSINESS_USER в той же организации физически получит
// событие, но клиент его не запросит.
//
// Provider в App-shell вызывает этот хук однократно — глобальная подписка.
// Параллельные `useEventStream(versionId, ...)` на странице
// ContractDetailPage (для polling-fallback по конкретной версии) безопасны:
// события приходят независимо, store идемпотентен (LRU recent гарантирует
// no-op для повторов).
import { useEventStream } from '@/shared/api';
import { useCan } from '@/shared/auth/rbac';

import { useLowConfidenceStore } from './low-confidence-store';

export function useLowConfidenceBridge(): void {
  const allowed = useCan('version.confirm-type');

  useEventStream(undefined, {
    enabled: allowed,
    onTypeConfirmation: (event) => {
      useLowConfidenceStore.getState().open(event);
    },
  });
}

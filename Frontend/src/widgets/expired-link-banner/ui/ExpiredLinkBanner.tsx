// ExpiredLinkBanner (FE-TASK-048) — баннер «ссылка истекла» на странице
// «Отчёты» (Figma 9, §17.4).
//
// Show-условие поднимается page-компонентом (читает `?share=expired` из URL).
// Виджет — полностью презентационный: принимает `visible`, `onDismiss`,
// опциональный `onRequestNew` (CTA «Запросить новую ссылку»). Сам searchParam
// чистит page.
//
// UX:
//   - role=status + aria-live=polite — скринридер сообщает о ссылке, но не
//     прерывает.
//   - tone=warning (не error) — ссылка протухла, но это не ошибка системы.
//   - Dismiss-кнопка — всегда присутствует; visibleProp контролирует рендер.
import { Button } from '@/shared/ui';

export interface ExpiredLinkBannerProps {
  visible: boolean;
  onDismiss: () => void;
  /** CTA «Запросить новую ссылку». Если не передан — кнопка не рендерится. */
  onRequestNew?: () => void;
}

export function ExpiredLinkBanner({
  visible,
  onDismiss,
  onRequestNew,
}: ExpiredLinkBannerProps): JSX.Element | null {
  if (!visible) return null;
  return (
    <section
      role="status"
      aria-live="polite"
      data-testid="expired-link-banner"
      className="flex flex-wrap items-start justify-between gap-3 rounded-md border border-warning/40 bg-[color-mix(in_srgb,var(--color-warning)_12%,transparent)] p-4 text-sm text-fg"
    >
      <div className="flex-1 min-w-[240px]">
        <p className="font-medium text-fg">Защищённая ссылка больше не действительна.</p>
        <p className="mt-1 text-fg-muted">
          Срок действия ссылки — 5 минут. Запросите у владельца новую или откройте отчёт из реестра.
        </p>
      </div>
      <div className="flex items-center gap-2">
        {onRequestNew ? (
          <Button
            type="button"
            variant="secondary"
            size="sm"
            onClick={onRequestNew}
            data-testid="expired-link-banner-retry"
          >
            Запросить новую ссылку
          </Button>
        ) : null}
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={onDismiss}
          data-testid="expired-link-banner-dismiss"
          aria-label="Скрыть сообщение о просроченной ссылке"
        >
          Скрыть
        </Button>
      </div>
    </section>
  );
}

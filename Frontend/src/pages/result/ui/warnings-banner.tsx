// WarningsBanner — inline-баннер «Обнаружены предупреждения» для состояния
// PARTIALLY_FAILED (§17.5 PROCESSING_WARNINGS). Договор прошёл анализ, но
// часть текста распознана с низкой уверенностью или часть артефактов не
// сгенерирована. Показывается поверх основного контента READY.
export interface WarningsBannerProps {
  /** Текстовое сообщение с бэкенда (processing_status_message). */
  message?: string | undefined;
}

export function WarningsBanner({ message }: WarningsBannerProps): JSX.Element {
  return (
    <aside
      role="status"
      data-testid="warnings-banner"
      className="flex flex-col gap-1 rounded-md border border-warning/50 bg-[color-mix(in_srgb,var(--color-warning)_18%,transparent)] p-4"
    >
      <p className="text-sm font-semibold text-fg">Часть результатов требует внимания</p>
      <p className="text-xs text-fg">
        {message ??
          'Некоторые артефакты сформированы частично (например, часть текста распознана OCR с низкой уверенностью). Проверьте детали перед подписанием договора.'}
      </p>
    </aside>
  );
}

/**
 * Placeholder для /contracts/new (FE-TASK-031). Финальная имплементация — FE-TASK-043
 * (FileDropZone + PasteTextTab + WillHappenSteps + WhatWeCheck; 12 состояний Figma).
 */
export function NewCheckPage(): JSX.Element {
  return (
    <main
      data-testid="page-new-check"
      className="mx-auto flex min-h-[60vh] max-w-3xl flex-col gap-3 px-6 py-12"
    >
      <h1 className="text-2xl font-semibold text-fg">Новая проверка</h1>
      <p className="text-base text-fg-muted">
        Загрузка договора появится в FE-TASK-043. Сейчас доступен только маршрут.
      </p>
    </main>
  );
}

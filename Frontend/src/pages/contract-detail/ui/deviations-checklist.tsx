// DeviationsChecklist — отклонения от политики организации (экран 8 Figma,
// §17.4). В v1 настоящие политики/чек-листы подключаются через
// features/policy-edit + features/checklist-edit (ORG_ADMIN-only), а сопоставление
// с конкретным договором — из анализа LIC (FE-TASK-046 ResultPage).
// Здесь — плейсхолдер с пустым состоянием.
export function DeviationsChecklist(): JSX.Element {
  return (
    <section
      aria-label="Отклонения от политики"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header>
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Отклонения от политики
        </h2>
        <p className="mt-1 text-xs text-fg-muted">
          Сопоставление с корпоративными чек-листами и политиками
        </p>
      </header>
      <p className="text-sm text-fg-muted">
        Отклонения отобразятся после завершения анализа. Настраивать политики может администратор
        организации в разделе «Политики».
      </p>
    </section>
  );
}

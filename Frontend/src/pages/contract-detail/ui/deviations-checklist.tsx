// DeviationsChecklist — отклонения от шаблона организации (Figma 306:2 → 315:48).
// Honest-плейсхолдер: сопоставление договора с корпоративными чек-листами/
// политиками приходит из анализа LIC (FE-TASK-046) — здесь данных нет → empty.
// aria-label «Отклонения от политики» сохранён (от него зависит RBAC-тест +
// единообразие с §5.6 Pattern B). Card flat-border treatment.
import { Card } from '@/shared/ui';

export function DeviationsChecklist(): JSX.Element {
  return (
    <Card
      as="section"
      aria-label="Отклонения от политики"
      radius="xl"
      className="flex flex-col gap-3 border border-border-subtle px-7 py-6 shadow-none"
    >
      <h2 className="text-18 font-semibold text-fg">Отклонения от шаблона организации</h2>
      <p className="text-14 leading-5 text-fg-muted">
        Отклонения отобразятся после завершения анализа. Настраивать политики может администратор
        организации в разделе «Политики».
      </p>
    </Card>
  );
}

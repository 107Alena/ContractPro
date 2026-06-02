// AccessNote — блок «Доступ и ограничения» правой колонки карточки договора
// (Figma 306:2 → Access Note 312:53). Статичный honest-контент: фиксирует
// границы доступа и характер анализа. Декоративный замок — aria-hidden.
import { Card } from '@/shared/ui';

const NOTES: readonly string[] = [
  'Доступ ограничен рамками организации',
  'Экспорт регулируется политикой',
  'Рекомендательный характер анализа',
];

export function AccessNote(): JSX.Element {
  return (
    <Card
      as="section"
      aria-label="Доступ и ограничения"
      radius="lg"
      className="flex flex-col gap-3 border border-border-subtle p-5 shadow-none"
    >
      <h2 className="flex items-center gap-1.5 text-14 font-medium text-fg">
        <span aria-hidden>🔒</span>
        Доступ и ограничения
      </h2>
      <ul className="flex flex-col gap-2">
        {NOTES.map((note) => (
          <li key={note} className="flex gap-1.5 text-13 text-fg-muted">
            <span aria-hidden className="text-fg-disabled">
              ·
            </span>
            {note}
          </li>
        ))}
      </ul>
    </Card>
  );
}

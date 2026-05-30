// TrustFooter — нижняя «доверительная» строка dashboard (Figma 84:2 → 92:53).
// Статичные trust-маркеры (шифрование, юрисдикция, рекомендательный характер,
// ролевой доступ). Без данных — чистая статика.
const ITEMS = [
  '🔒 Данные зашифрованы',
  '📋 Юрисдикция РФ',
  'ⓘ Рекомендательный характер анализа',
  '👤 Доступ ограничен ролью',
] as const;

export function TrustFooter(): JSX.Element {
  return (
    <div className="flex flex-wrap items-center justify-center gap-x-6 gap-y-2 pb-2 pt-4 text-11 text-fg-disabled">
      {ITEMS.map((item) => (
        <span key={item}>{item}</span>
      ))}
    </div>
  );
}

// TrustFooter — нижняя «доверительная» строка dashboard (Figma 84:2 → 92:53).
// Статичные trust-маркеры. Декоративные эмодзи помечены aria-hidden, чтобы
// скринридер не озвучивал их unicode-имена (особенно ⓘ).
const ITEMS = [
  { icon: '🔒', text: 'Данные зашифрованы' },
  { icon: '📋', text: 'Юрисдикция РФ' },
  { icon: 'ⓘ', text: 'Рекомендательный характер анализа' },
  { icon: '👤', text: 'Доступ ограничен ролью' },
] as const;

export function TrustFooter(): JSX.Element {
  return (
    <div className="flex flex-wrap items-center justify-center gap-x-6 gap-y-2 pb-2 pt-4 text-11 text-fg-disabled">
      {ITEMS.map(({ icon, text }) => (
        <span key={text} className="inline-flex items-center gap-1.5">
          <span aria-hidden="true">{icon}</span>
          {text}
        </span>
      ))}
    </div>
  );
}

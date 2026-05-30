// HeroProductMockup — статичная композиция, имитирующая интерфейс продукта
// для Hero-секции (Figma node 12:13 ProductMockup). Презентационный
// «скриншот»: sidebar с риск-профилем + main content с резюме и параметрами.
// Чисто визуальный элемент — без интерактивности, доменной логики и a11y-роли
// (aria-hidden, чтобы скринридер не дублировал контент CTA).

import { Badge } from '@/shared/ui';

const RISK_ITEMS: ReadonlyArray<{ id: string; label: string; level: 'high' | 'medium' | 'low' }> = [
  { id: 'liability', label: 'Нет ограничения ответственности', level: 'high' },
  { id: 'autorenewal', label: 'Автоматическая пролонгация', level: 'high' },
  { id: 'payment-term', label: 'Срок оплаты не определён', level: 'medium' },
  { id: 'requisites', label: 'Неполные реквизиты', level: 'low' },
];

const KEY_PARAMS: ReadonlyArray<{ id: string; label: string; value: string }> = [
  { id: 'type', label: 'Тип договора', value: 'Оказание услуг' },
  { id: 'parties', label: 'Стороны', value: 'ООО «Альфа» → ИП Петров' },
  { id: 'term', label: 'Срок', value: '12 месяцев' },
  { id: 'amount', label: 'Сумма', value: '480 000 ₽' },
];

const DOT_BY_LEVEL: Record<'high' | 'medium' | 'low', string> = {
  high: 'bg-risk-high',
  medium: 'bg-risk-medium',
  low: 'bg-risk-low',
};

export function HeroProductMockup(): JSX.Element {
  return (
    <div
      aria-hidden="true"
      className="grid w-full max-w-[1120px] grid-cols-1 overflow-hidden rounded-xl border border-border-subtle bg-bg shadow-card lg:grid-cols-[300px_1fr]"
    >
      {/* Sidebar — Риск-профиль */}
      <aside className="flex flex-col gap-3.5 bg-bg-muted p-6">
        <h3 className="text-16 font-semibold text-fg">Риск-профиль</h3>
        <div className="flex items-center gap-3">
          <div className="size-12 rounded-full bg-risk-medium" />
          <div className="flex flex-col gap-0.5">
            <p className="text-14 font-semibold text-risk-medium">Средний риск</p>
            <p className="text-13 text-fg-subtle">Найдено 4 замечания</p>
          </div>
        </div>
        <ul className="flex flex-col gap-2.5">
          {RISK_ITEMS.map((item) => (
            <li
              key={item.id}
              className="flex items-center gap-2.5 rounded-md bg-bg px-3 py-2.5 text-13 font-medium text-fg-strong"
            >
              <span className={`size-2 shrink-0 rounded-full ${DOT_BY_LEVEL[item.level]}`} />
              {item.label}
            </li>
          ))}
        </ul>
      </aside>

      {/* Main content */}
      <div className="flex flex-col gap-5 px-8 py-6">
        <div className="flex flex-wrap items-center gap-3">
          <p className="text-15 font-medium text-fg">Договор_оказания_услуг_2026.pdf</p>
          <Badge variant="brand" size="sm">
            Проверен
          </Badge>
        </div>

        <div className="flex flex-col gap-2 rounded-xl bg-bg-muted px-5 py-4">
          <p className="text-15 font-semibold text-fg">Краткое резюме</p>
          <p className="text-14 leading-[22px] text-fg-muted">
            Договор оказания услуг между ООО «Альфа» и ИП Петров. Срок действия — 12 мес. с
            автопролонгацией. Обнаружены существенные риски: отсутствие ограничения ответственности
            и неопределённый срок оплаты.
          </p>
        </div>

        <p className="text-15 font-semibold text-fg">Ключевые параметры</p>
        <ul className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          {KEY_PARAMS.map((p) => (
            <li
              key={p.id}
              className="flex flex-col gap-1 rounded-md border border-border-subtle bg-bg px-3.5 py-2.5"
            >
              <span className="text-12 text-fg-subtle">{p.label}</span>
              <span className="text-14 font-semibold text-fg">{p.value}</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

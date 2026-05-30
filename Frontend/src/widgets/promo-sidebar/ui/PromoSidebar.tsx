// PromoSidebar — Figma node 49:2 TrustPanel (Auth Page Desktop левая колонка).
// Gradient bg, Badge eyebrow, заголовок 36px, 4 trust-карточки и продуктовый
// teaser с примером результата проверки.
//
// На <md скрывается (mobile Figma не показывает sidebar).
//
// Статический контент без i18n до FE-TASK-030. Использует токены §8.2.

export interface PromoSidebarProps {
  className?: string;
}

interface TrustItem {
  id: string;
  emoji: string;
  line1: string;
  line2: string;
}

const TRUST_ITEMS: ReadonlyArray<TrustItem> = [
  { id: 'safe', emoji: '🛡', line1: 'Безопасная работа', line2: 'с документами' },
  { id: 'roles', emoji: '👥', line1: 'Доступ в рамках', line2: 'организации и роли' },
  { id: 'jurisdiction', emoji: '⚖️', line1: 'Поддержка договорной', line2: 'практики РФ' },
  { id: 'advisory', emoji: '💡', line1: 'Рекомендательный', line2: 'характер анализа' },
];

interface RiskFinding {
  id: string;
  label: string;
  level: 'high' | 'medium';
}

const RISK_FINDINGS: ReadonlyArray<RiskFinding> = [
  { id: 'liability', label: 'Нет ограничения ответственности', level: 'high' },
  { id: 'requisites', label: 'Неполные реквизиты сторон', level: 'high' },
  { id: 'autorenewal', label: 'Автоматическая пролонгация', level: 'medium' },
  { id: 'payment', label: 'Срок оплаты не определён', level: 'medium' },
];

const KEY_PARAMS: ReadonlyArray<{ id: string; label: string; value: string }> = [
  { id: 'type', label: 'Тип', value: 'Оказание услуг' },
  { id: 'parties', label: 'Стороны', value: 'ООО «Альфа» → ИП Петров' },
  { id: 'term', label: 'Срок', value: '12 месяцев' },
  { id: 'amount', label: 'Сумма', value: '450 000 ₽' },
];

export function PromoSidebar({ className }: PromoSidebarProps): JSX.Element {
  return (
    <aside
      aria-label="ContractPro — проверка договоров"
      className={[
        'hidden md:flex',
        'flex-col gap-10 px-10 py-12 lg:px-20 lg:py-16',
        'bg-gradient-to-br from-[#faf7f5] via-[#f5f6fa] to-[#f9f5f2]',
        className ?? '',
      ].join(' ')}
      data-testid="promo-sidebar"
    >
      <header className="flex flex-col gap-4">
        <span className="inline-flex w-fit items-center rounded-full bg-brand-50 px-3.5 py-1.5 text-13 font-medium text-brand-600">
          AI-платформа для договорной работы
        </span>
        <h2 className="max-w-md text-[36px] font-bold leading-[44px] text-fg">
          Проверяйте договоры быстрее и без рисков
        </h2>
        <p className="max-w-sm text-16 leading-6 text-fg-muted">
          Находите юридические риски, получайте понятные рекомендации и экономьте время на
          согласование документов
        </p>
      </header>

      <ul className="flex flex-wrap gap-3">
        {TRUST_ITEMS.map((item) => (
          <li
            key={item.id}
            className="flex items-start gap-2.5 rounded-xl bg-white/70 px-3.5 py-3 shadow-[0_1px_4px_0_rgba(0,0,0,0.04)]"
          >
            <span
              aria-hidden="true"
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-2xl bg-brand-50 text-16"
            >
              {item.emoji}
            </span>
            <div className="flex flex-col text-13 font-medium leading-[18px] text-fg-strong">
              <span>{item.line1}</span>
              <span>{item.line2}</span>
            </div>
          </li>
        ))}
      </ul>

      {/* ProductTeaser — статичный «скриншот» результата проверки */}
      <div
        aria-hidden="true"
        className="flex flex-col overflow-hidden rounded-xl bg-bg shadow-[0_8px_24px_-4px_rgba(0,0,0,0.06),0_2px_8px_0_rgba(0,0,0,0.04)]"
      >
        <div className="flex items-center justify-between px-6 pb-3.5 pt-4">
          <p className="text-14 font-semibold text-fg">Результат проверки</p>
          <span className="rounded-md bg-[color-mix(in_srgb,var(--color-brand-500)_12%,transparent)] px-2.5 py-1 text-12 font-semibold text-brand-600">
            Средний риск
          </span>
        </div>
        <div className="h-px w-full bg-divider" />
        <div className="grid grid-cols-2 gap-5 px-6 pb-5 pt-4">
          <div className="flex flex-col gap-2.5">
            <p className="text-12 font-semibold text-fg-subtle">Найденные замечания</p>
            {RISK_FINDINGS.map((f) => (
              <div key={f.id} className="flex items-center gap-2">
                <span
                  className={`size-1.5 shrink-0 rounded-full ${f.level === 'high' ? 'bg-risk-high' : 'bg-risk-medium'}`}
                />
                <p className="text-12 font-medium text-fg-strong">{f.label}</p>
              </div>
            ))}
          </div>
          <div className="flex flex-col gap-3">
            <p className="text-12 font-semibold text-fg-subtle">Ключевые параметры</p>
            {KEY_PARAMS.map((p) => (
              <div key={p.id} className="flex flex-col gap-0.5">
                <span className="text-11 text-fg-subtle">{p.label}</span>
                <span className="text-12 font-semibold text-fg">{p.value}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </aside>
  );
}

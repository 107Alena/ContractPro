// FeaturesSection — сетка карточек «что умеет ContractPro». Responsive:
// 1 колонка < md, 2 колонки md–lg, 3 колонки lg+. Иконки — inline SVG (см. icons.tsx).
import { type FeatureCard, FEATURES } from '../content';
import { FeatureIcon } from './icons';

export interface FeaturesSectionProps {
  items?: FeatureCard[];
}

export function FeaturesSection({ items = FEATURES }: FeaturesSectionProps): JSX.Element {
  return (
    <section
      id="features"
      aria-labelledby="features-title"
      className="bg-bg py-16 md:py-20 lg:py-24"
    >
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-10 px-4">
        <header className="flex flex-col gap-3 text-center">
          <p className="text-sm font-semibold uppercase tracking-wider text-brand-600">
            Возможности
          </p>
          <h2 id="features-title" className="text-2xl font-semibold text-fg md:text-3xl">
            Всё необходимое для быстрой проверки договора
          </h2>
          <p className="mx-auto max-w-2xl text-base text-fg-muted">
            От распознавания PDF до готового отчёта — без переключения между инструментами.
          </p>
        </header>

        <ul className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
          {items.map((item) => (
            <li
              key={item.id}
              className="flex flex-col gap-3 rounded-lg border border-border bg-bg p-6 shadow-sm transition-colors hover:border-brand-500"
            >
              <span
                aria-hidden="true"
                className="inline-flex h-10 w-10 items-center justify-center rounded-md bg-brand-50 text-brand-600"
              >
                <FeatureIcon id={item.icon} />
              </span>
              <h3 className="text-base font-semibold text-fg">{item.title}</h3>
              <p className="text-sm text-fg-muted">{item.description}</p>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

// FeaturesSection — Figma node 14:2. 7 карточек с emoji-иконками, 4+3 layout
// на desktop, адаптив до 1 col на mobile. Каждая карточка: emoji в brand-pill
// квадрате + title + description.
import { type FeatureCard, FEATURES } from '../content';

export interface FeaturesSectionProps {
  items?: FeatureCard[];
}

export function FeaturesSection({ items = FEATURES }: FeaturesSectionProps): JSX.Element {
  return (
    <section
      id="features"
      aria-labelledby="features-title"
      className="bg-bg-muted px-4 py-16 sm:py-20 lg:px-20"
    >
      <div className="mx-auto flex w-full max-w-[1280px] flex-col items-center gap-12">
        <header className="flex flex-col items-center gap-4 text-center">
          <p className="text-14 font-semibold tracking-[2px] text-brand-500">ВОЗМОЖНОСТИ</p>
          <h2
            id="features-title"
            className="text-3xl font-bold leading-[1.1] tracking-[-0.5px] text-fg sm:text-4xl md:text-[44px] md:tracking-[-1px]"
          >
            Что умеет ContractPro
          </h2>
          <p className="max-w-2xl text-18 text-fg-muted">
            Полный набор инструментов для быстрой и безопасной работы с договорами
          </p>
        </header>

        {/* Flex+wrap+justify-center вместо grid — последний ряд из 3 карточек
            (при 7 cards / 4 cols) центрируется автоматически. */}
        <ul className="flex w-full flex-wrap justify-center gap-5">
          {items.map((item) => (
            <li
              key={item.id}
              className="flex w-full flex-col gap-3 rounded-xl bg-bg p-7 shadow-card sm:w-[calc(50%-10px)] lg:w-[calc((100%-40px)/3)] xl:w-[calc((100%-60px)/4)]"
            >
              <span
                aria-hidden="true"
                className="flex h-12 w-12 items-center justify-center rounded-xl bg-brand-50 text-2xl"
              >
                {item.emoji}
              </span>
              <h3 className="text-18 font-semibold text-fg">{item.title}</h3>
              <p className="text-15 leading-[23px] text-fg-muted">{item.description}</p>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

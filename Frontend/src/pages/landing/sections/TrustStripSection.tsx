// TrustStripSection — Figma node 13:2 (Trust Strip). Горизонтальная полоса
// из 4 trust-итемов: icon (emoji в brand-pill) + title + subtitle.
// Размещается на LandingPage между Hero и Features. На mobile —
// stacked grid (2x2 → 1col).
import { TRUST_ITEMS, type TrustItem } from '../content';

export interface TrustStripSectionProps {
  items?: ReadonlyArray<TrustItem>;
}

function TrustItemView({ item }: { item: TrustItem }): JSX.Element {
  return (
    <li className="flex items-center gap-3.5">
      <span
        aria-hidden="true"
        className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-brand-50 text-20"
      >
        {item.emoji}
      </span>
      <div className="flex flex-col gap-0.5">
        <p className="text-15 font-semibold text-fg">{item.title}</p>
        <p className="text-14 text-fg-subtle">{item.description}</p>
      </div>
    </li>
  );
}

export function TrustStripSection({ items = TRUST_ITEMS }: TrustStripSectionProps): JSX.Element {
  return (
    <section
      id="trust"
      aria-label="Ключевые преимущества"
      className="bg-bg px-4 py-10 sm:py-12 lg:px-20"
    >
      <ul className="mx-auto grid w-full max-w-[1280px] grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4 lg:gap-10">
        {items.map((item) => (
          <TrustItemView key={item.id} item={item} />
        ))}
      </ul>
    </section>
  );
}

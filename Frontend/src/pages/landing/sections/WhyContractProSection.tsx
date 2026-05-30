// WhyContractProSection — Figma node 22:2. 6 простых карточек 3+3 grid.
// Каждая: bg-bg-muted rounded-xl p-7, title 18px semibold + description
// 15px leading-24 fg-muted.
import { WHY_REASONS, type WhyReason } from '../content';

export interface WhyContractProSectionProps {
  reasons?: ReadonlyArray<WhyReason>;
}

export function WhyContractProSection({
  reasons = WHY_REASONS,
}: WhyContractProSectionProps): JSX.Element {
  return (
    <section id="why" aria-labelledby="why-title" className="bg-bg px-4 py-16 sm:py-20 lg:px-20">
      <div className="mx-auto flex w-full max-w-[1280px] flex-col items-center gap-12">
        <header className="flex flex-col items-center gap-4 text-center">
          <p className="text-14 font-semibold tracking-[2px] text-brand-500">ПОЧЕМУ МЫ</p>
          <h2
            id="why-title"
            className="text-3xl font-bold leading-[1.1] tracking-[-0.5px] text-fg sm:text-4xl md:text-[44px] md:tracking-[-1px]"
          >
            Почему выбирают ContractPro
          </h2>
        </header>

        <ul className="grid w-full grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
          {reasons.map((reason) => (
            <li key={reason.id} className="flex flex-col gap-2.5 rounded-xl bg-bg-muted p-7">
              <h3 className="text-18 font-semibold text-fg">{reason.title}</h3>
              <p className="text-15 leading-6 text-fg-muted">{reason.description}</p>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

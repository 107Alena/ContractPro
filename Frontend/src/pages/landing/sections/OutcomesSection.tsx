// OutcomesSection — Figma node 17:2. 4 metric-карточки с большой цифрой
// brand-500 (text-48 bold tracking -1.5) + описание под ней.
import { type OutcomeMetric, OUTCOMES } from '../content';

export interface OutcomesSectionProps {
  metrics?: ReadonlyArray<OutcomeMetric>;
}

export function OutcomesSection({ metrics = OUTCOMES }: OutcomesSectionProps): JSX.Element {
  return (
    <section
      id="outcomes"
      aria-labelledby="outcomes-title"
      className="bg-bg px-4 py-16 sm:py-20 lg:px-20"
    >
      <div className="mx-auto flex w-full max-w-[1280px] flex-col items-center gap-14">
        <header className="flex flex-col items-center gap-4 text-center">
          <p className="text-14 font-semibold tracking-[2px] text-brand-500">РЕЗУЛЬТАТЫ</p>
          <h2
            id="outcomes-title"
            className="text-3xl font-bold leading-[1.1] tracking-[-0.5px] text-fg sm:text-4xl md:text-[44px] md:tracking-[-1px]"
          >
            Что вы получаете с ContractPro
          </h2>
        </header>

        <ul className="grid w-full grid-cols-1 gap-8 sm:grid-cols-2 lg:grid-cols-4">
          {metrics.map((m) => (
            <li key={m.id} className="flex flex-col gap-3 rounded-[20px] bg-bg-muted px-8 py-9">
              <p className="whitespace-nowrap text-[48px] font-bold leading-none tracking-[-1.5px] text-brand-500">
                {m.metric}
              </p>
              <p className="text-16 leading-6 text-fg-muted">{m.label}</p>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

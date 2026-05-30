// HowItWorksSection — Figma node 15:2. 4 step-карточки 1-2-3-4 в ряд
// с большой цифрой brand-500@20% opacity сверху, title 20px semibold,
// description 15px fg-muted.
import { HOW_IT_WORKS_STEPS, type HowItWorksStep } from '../content';

export interface HowItWorksSectionProps {
  steps?: ReadonlyArray<HowItWorksStep>;
}

export function HowItWorksSection({
  steps = HOW_IT_WORKS_STEPS,
}: HowItWorksSectionProps): JSX.Element {
  return (
    <section
      id="how-it-works"
      aria-labelledby="how-it-works-title"
      className="bg-bg px-4 py-16 sm:py-20 lg:px-20"
    >
      <div className="mx-auto flex w-full max-w-[1280px] flex-col items-center gap-14">
        <header className="flex flex-col items-center gap-4 text-center">
          <p className="text-14 font-semibold tracking-[2px] text-brand-500">ПРОЦЕСС</p>
          <h2
            id="how-it-works-title"
            className="text-3xl font-bold leading-[1.1] tracking-[-0.5px] text-fg sm:text-4xl md:text-[44px] md:tracking-[-1px]"
          >
            Как это работает
          </h2>
        </header>

        <ol className="grid w-full grid-cols-1 gap-8 sm:grid-cols-2 lg:grid-cols-4">
          {steps.map((step) => (
            <li key={step.id} className="flex flex-col gap-4 rounded-xl bg-bg-muted px-7 py-8">
              <p
                aria-hidden="true"
                className="text-[40px] font-bold leading-none text-brand-500/20"
              >
                {step.number}
              </p>
              <h3 className="text-20 font-semibold text-fg">
                <span className="sr-only">{`Шаг ${step.number}: `}</span>
                {step.title}
              </h3>
              <p className="text-15 leading-[23px] text-fg-muted">{step.description}</p>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
}

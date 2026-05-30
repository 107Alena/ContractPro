// ForWhomSection — Figma node 16:2. 3 audience-карточки в ряд: МСП /
// Юридические департаменты / Закупки и продажи. Каждая карточка — большая
// emoji-икона, title 22px, description 15px, 3 check-bullets с зелёной
// галочкой ✓.
import { type AudienceCard, AUDIENCES } from '../content';

export interface ForWhomSectionProps {
  cards?: ReadonlyArray<AudienceCard>;
}

function AudienceCardView({ card }: { card: AudienceCard }): JSX.Element {
  return (
    <li className="flex flex-col gap-5 rounded-[20px] bg-bg p-8 shadow-card">
      <span
        aria-hidden="true"
        className="flex h-14 w-14 items-center justify-center rounded-2xl bg-brand-50 text-[28px]"
      >
        {card.emoji}
      </span>
      <h3 className="text-[22px] font-semibold text-fg">{card.title}</h3>
      <p className="text-15 leading-6 text-fg-muted">{card.description}</p>
      <ul className="flex flex-col gap-2.5 text-14">
        {card.bullets.map((bullet) => (
          <li key={bullet} className="flex items-center gap-2.5">
            <span aria-hidden="true" className="font-semibold text-success">
              ✓
            </span>
            <span className="text-fg-muted">{bullet}</span>
          </li>
        ))}
      </ul>
    </li>
  );
}

export function ForWhomSection({ cards = AUDIENCES }: ForWhomSectionProps): JSX.Element {
  return (
    <section
      id="for-whom"
      aria-labelledby="for-whom-title"
      className="bg-bg-muted px-4 py-16 sm:py-20 lg:px-20"
    >
      <div className="mx-auto flex w-full max-w-[1280px] flex-col items-center gap-12">
        <header className="flex flex-col items-center gap-4 text-center">
          <p className="text-14 font-semibold tracking-[2px] text-brand-500">АУДИТОРИЯ</p>
          <h2
            id="for-whom-title"
            className="text-3xl font-bold leading-[1.1] tracking-[-0.5px] text-fg sm:text-4xl md:text-[44px] md:tracking-[-1px]"
          >
            Для кого ContractPro
          </h2>
        </header>

        <ul className="grid w-full grid-cols-1 gap-6 lg:grid-cols-3">
          {cards.map((card) => (
            <AudienceCardView key={card.id} card={card} />
          ))}
        </ul>
      </div>
    </section>
  );
}

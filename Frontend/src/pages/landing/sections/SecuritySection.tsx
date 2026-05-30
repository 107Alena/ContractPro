// SecuritySection — Figma node 18:2. ТЁМНАЯ секция с 2-колоночной разводкой:
// слева — заголовок и описание; справа — 4 feature-карточки с
// semi-transparent white bg.
import { SECURITY_FEATURES, type SecurityFeature } from '../content';

export interface SecuritySectionProps {
  features?: ReadonlyArray<SecurityFeature>;
}

function SecurityCard({ feature }: { feature: SecurityFeature }): JSX.Element {
  return (
    <li className="flex items-start gap-4 rounded-[14px] bg-white/[0.06] p-5">
      <span aria-hidden="true" className="shrink-0 text-2xl">
        {feature.emoji}
      </span>
      <div className="flex flex-1 flex-col gap-1">
        <p className="text-16 font-semibold text-white">{feature.title}</p>
        <p className="text-14 leading-[22px] text-fg-disabled">{feature.description}</p>
      </div>
    </li>
  );
}

export function SecuritySection({
  features = SECURITY_FEATURES,
}: SecuritySectionProps): JSX.Element {
  return (
    <section
      id="security"
      aria-labelledby="security-title"
      className="bg-fg px-4 py-16 sm:py-20 lg:px-20 lg:py-[72px]"
    >
      <div className="mx-auto grid w-full max-w-[1280px] grid-cols-1 gap-12 lg:grid-cols-2 lg:gap-16">
        <div className="flex flex-col gap-6">
          <p className="text-14 font-semibold tracking-[2px] text-brand-500">БЕЗОПАСНОСТЬ</p>
          <h2
            id="security-title"
            className="text-3xl font-bold leading-[1.2] tracking-[-0.5px] text-white sm:text-4xl md:text-[40px] md:leading-[48px] md:tracking-[-1px]"
          >
            Ваши документы под защитой
          </h2>
          <p className="text-16 leading-[26px] text-fg-disabled">
            ContractPro — это инструмент поддержки, а не замена юридического заключения. Мы
            обеспечиваем конфиденциальность и прозрачность на каждом шаге.
          </p>
        </div>

        <ul className="flex flex-col gap-4">
          {features.map((f) => (
            <SecurityCard key={f.id} feature={f} />
          ))}
        </ul>
      </div>
    </section>
  );
}

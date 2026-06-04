// CtaBannerSection — Figma node 24:2 (CTA Banner). Финальный CTA-блок:
// большая ТЁМНАЯ карточка bg-fg rounded-3xl, белый заголовок 40px,
// subtitle fg-disabled, 2 больших CTA (brand-500 primary + outlined white).
import { Link } from 'react-router-dom';

import { cn } from '@/shared/lib/cn';
import { buttonVariants } from '@/shared/ui';

const CTA_OVERRIDE = 'h-auto px-8 py-4 text-17 rounded-xl';

export function CtaBannerSection(): JSX.Element {
  return (
    <section
      id="cta-banner"
      aria-label="Призыв к действию"
      className="bg-bg px-4 py-16 sm:py-20 lg:px-20"
    >
      <div className="mx-auto flex w-full max-w-[1280px] flex-col items-center justify-center gap-7 rounded-3xl bg-fg px-6 py-12 sm:px-12 sm:py-16 lg:px-20 lg:py-16">
        <h2 className="text-3xl font-bold leading-[1.2] tracking-[-0.5px] text-white sm:text-4xl md:text-[40px] md:leading-[48px] md:tracking-[-1px]">
          <span className="block text-center">Начните проверять договоры</span>
          <span className="block text-center">уже сегодня</span>
        </h2>
        <p className="text-center text-18 text-fg-disabled">
          3 бесплатные проверки. Без привязки карты. Результат за минуты.
        </p>
        <div className="mt-2 flex flex-col items-stretch gap-3 sm:flex-row sm:items-center sm:justify-center sm:gap-4">
          <Link
            to="/login"
            className={cn(
              buttonVariants({ variant: 'primary', size: 'lg' }),
              CTA_OVERRIDE,
              'shadow-[0_4px_20px_0_rgba(245,94,18,0.3)]',
            )}
          >
            Попробовать бесплатно
          </Link>
        </div>
      </div>
    </section>
  );
}

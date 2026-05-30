// Inline SVG-иконки для WelcomeBlock CTA. Self-contained на уровне слайса
// (централизованного набора shared/ui/icons ещё нет) — аналогично topbar/icons.tsx.
import type { SVGProps } from 'react';

type IconProps = SVGProps<SVGSVGElement>;

const baseProps: Partial<IconProps> = {
  width: 18,
  height: 18,
  viewBox: '0 0 20 20',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.5,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
  'aria-hidden': true,
  focusable: false,
};

// Новая проверка — лупа (анализ договора).
export function CheckScanIcon(props: IconProps): JSX.Element {
  return (
    <svg {...baseProps} {...props}>
      <circle cx="9" cy="9" r="5.5" />
      <path d="M13 13l4 4" />
    </svg>
  );
}

// Загрузить договор — стрелка вверх в лоток.
export function UploadIcon(props: IconProps): JSX.Element {
  return (
    <svg {...baseProps} {...props}>
      <path d="M10 3v9" />
      <path d="M6.5 6.5 10 3l3.5 3.5" />
      <path d="M4 13.5V16h12v-2.5" />
    </svg>
  );
}

// Вставить текст — буфер обмена со строками.
export function PasteIcon(props: IconProps): JSX.Element {
  return (
    <svg {...baseProps} {...props}>
      <rect x="5" y="4.5" width="10" height="12.5" rx="1.5" />
      <path d="M8 4.5V3.5h4v1" />
      <path d="M7.5 9.5h5M7.5 12.5h5" />
    </svg>
  );
}

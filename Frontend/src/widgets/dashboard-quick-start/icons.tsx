// Inline SVG-иконки быстрых действий QuickStart. Self-contained на уровне слайса
// (нет общего shared/ui/icons), аналогично topbar/icons.tsx.
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

export function ScanIcon(props: IconProps): JSX.Element {
  return (
    <svg {...baseProps} {...props}>
      <circle cx="9" cy="9" r="5.5" />
      <path d="m13 13 4 4" />
    </svg>
  );
}

export function UploadIcon(props: IconProps): JSX.Element {
  return (
    <svg {...baseProps} {...props}>
      <path d="M10 3v9" />
      <path d="M6.5 6.5 10 3l3.5 3.5" />
      <path d="M4 13.5V16h12v-2.5" />
    </svg>
  );
}

export function PasteIcon(props: IconProps): JSX.Element {
  return (
    <svg {...baseProps} {...props}>
      <rect x="5" y="4.5" width="10" height="12.5" rx="1.5" />
      <path d="M8 4.5V3.5h4v1" />
      <path d="M7.5 9.5h5M7.5 12.5h5" />
    </svg>
  );
}

export function CompareIcon(props: IconProps): JSX.Element {
  return (
    <svg {...baseProps} {...props}>
      <path d="M5 7h9l-2.5-2.5M15 13H6l2.5 2.5" />
    </svg>
  );
}

export function DocsIcon(props: IconProps): JSX.Element {
  return (
    <svg {...baseProps} {...props}>
      <rect x="4.5" y="4" width="11" height="12" rx="1.5" />
      <path d="M7.5 8h5M7.5 11h5M7.5 14h3" />
    </svg>
  );
}

export function DownloadIcon(props: IconProps): JSX.Element {
  return (
    <svg {...baseProps} {...props}>
      <path d="M10 4v8" />
      <path d="M6.5 8.5 10 12l3.5-3.5" />
      <path d="M4 14.5V16h12v-1.5" />
    </svg>
  );
}

export function ShareIcon(props: IconProps): JSX.Element {
  return (
    <svg {...baseProps} {...props}>
      <circle cx="6" cy="10" r="2" />
      <circle cx="14" cy="5" r="2" />
      <circle cx="14" cy="15" r="2" />
      <path d="m7.8 9 4.4-2.8M7.8 11l4.4 2.8" />
    </svg>
  );
}

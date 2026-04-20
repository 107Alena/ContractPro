// Inline SVG-иконки для Topbar. Не импортируются из shared/ui/icons (ещё не
// существует централизованного набора) — содержимое self-contained на уровне
// слайса, аналогично sidebar-navigation/icons.tsx.
import type { SVGProps } from 'react';

type IconProps = SVGProps<SVGSVGElement>;

const baseProps: Partial<IconProps> = {
  width: 20,
  height: 20,
  viewBox: '0 0 20 20',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.5,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
  'aria-hidden': true,
  focusable: false,
};

export function ChevronDownIcon(props: IconProps): JSX.Element {
  return (
    <svg {...baseProps} {...props}>
      <path d="M5 7.5l5 5 5-5" />
    </svg>
  );
}

export function MenuIcon(props: IconProps): JSX.Element {
  return (
    <svg {...baseProps} {...props}>
      <path d="M3 6h14M3 10h14M3 14h14" />
    </svg>
  );
}

export function BellIcon(props: IconProps): JSX.Element {
  return (
    <svg {...baseProps} {...props}>
      <path d="M10 3.5a4 4 0 0 0-4 4V10l-1.5 2.5h11L14 10V7.5a4 4 0 0 0-4-4Z" />
      <path d="M8.5 15a1.5 1.5 0 0 0 3 0" />
    </svg>
  );
}

export function WifiOffIcon(props: IconProps): JSX.Element {
  return (
    <svg {...baseProps} {...props}>
      <path d="M2 5l16 12" />
      <path d="M4.5 9.8a9 9 0 0 1 4.7-2.4" />
      <path d="M10 5a13 13 0 0 1 8 3" />
      <path d="M7 12.5a5 5 0 0 1 2.5-1.2" />
      <circle cx="10" cy="15.5" r="0.75" fill="currentColor" stroke="none" />
    </svg>
  );
}

export function UserCircleIcon(props: IconProps): JSX.Element {
  return (
    <svg {...baseProps} {...props}>
      <circle cx="10" cy="10" r="7.5" />
      <circle cx="10" cy="8" r="2.5" />
      <path d="M4.8 16.2a6 6 0 0 1 10.4 0" />
    </svg>
  );
}

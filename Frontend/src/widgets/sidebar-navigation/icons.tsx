// Иконки Sidebar — inline SVG (stroke-based, 24×24, currentColor).
// Соответствие Figma 'Icons / Nav' (fileKey Lxhk7jQyXL3iuoTpiOHxcb).
// aria-hidden="true" — семантика пункта формируется родителем через
// aria-label/sr-only label. Иконка визуальная, не информационная (WCAG SC 1.1.1).
import type { SVGProps } from 'react';

type IconProps = SVGProps<SVGSVGElement>;

const defaultSvgProps = {
  width: 20,
  height: 20,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.75,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
  'aria-hidden': true,
  focusable: false,
} as const;

export function DashboardIcon(props: IconProps): JSX.Element {
  return (
    <svg {...defaultSvgProps} {...props}>
      <rect x="3" y="3" width="7" height="9" />
      <rect x="14" y="3" width="7" height="5" />
      <rect x="14" y="12" width="7" height="9" />
      <rect x="3" y="16" width="7" height="5" />
    </svg>
  );
}

export function ContractsIcon(props: IconProps): JSX.Element {
  return (
    <svg {...defaultSvgProps} {...props}>
      <path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z" />
      <path d="M14 3v6h6" />
      <path d="M8 13h8" />
      <path d="M8 17h6" />
    </svg>
  );
}

export function ReportsIcon(props: IconProps): JSX.Element {
  return (
    <svg {...defaultSvgProps} {...props}>
      <path d="M3 3v18h18" />
      <path d="M7 15l4-5 4 3 5-7" />
    </svg>
  );
}

export function SettingsIcon(props: IconProps): JSX.Element {
  return (
    <svg {...defaultSvgProps} {...props}>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.6 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.6a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09A1.65 1.65 0 0 0 15 4.6a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9c.14.33.22.7.22 1.09V10a2 2 0 1 1 0 4 1.65 1.65 0 0 0-.22 1z" />
    </svg>
  );
}

export function PoliciesIcon(props: IconProps): JSX.Element {
  return (
    <svg {...defaultSvgProps} {...props}>
      <path d="M12 2 4 5v6c0 5 3.4 9.6 8 11 4.6-1.4 8-6 8-11V5z" />
      <path d="m9 12 2 2 4-4" />
    </svg>
  );
}

export function ChecklistIcon(props: IconProps): JSX.Element {
  return (
    <svg {...defaultSvgProps} {...props}>
      <rect x="4" y="4" width="16" height="16" rx="2" />
      <path d="m8 12 2 2 4-4" />
      <path d="M8 17h8" />
    </svg>
  );
}

export function ChevronLeftIcon(props: IconProps): JSX.Element {
  return (
    <svg {...defaultSvgProps} {...props}>
      <path d="m15 18-6-6 6-6" />
    </svg>
  );
}

export function CloseIcon(props: IconProps): JSX.Element {
  return (
    <svg {...defaultSvgProps} {...props}>
      <path d="M18 6 6 18" />
      <path d="m6 6 12 12" />
    </svg>
  );
}

export function BrandLogoIcon(props: IconProps): JSX.Element {
  // Компактный знак «CP» в брендовом круге. Используется как fallback-avatar
  // в collapsed rail, когда текст скрыт.
  return (
    <svg
      width={24}
      height={24}
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
      focusable={false}
      {...props}
    >
      <rect width="24" height="24" rx="6" fill="currentColor" />
      <path
        d="M9.6 15.4c-2 0-3.3-1.4-3.3-3.4 0-2 1.4-3.4 3.3-3.4 1.1 0 2 .4 2.5 1.1l-1 .9c-.4-.5-.9-.7-1.5-.7-1.1 0-1.9.9-1.9 2.1s.8 2.1 1.9 2.1c.6 0 1.1-.2 1.5-.7l1 .9c-.5.7-1.4 1.1-2.5 1.1zm4.4-.1h-1.3V8.7h2.6c1.5 0 2.5.9 2.5 2.3 0 1.4-1 2.3-2.5 2.3H14v2zm0-3.3h1.2c.8 0 1.2-.4 1.2-1.1 0-.7-.5-1.1-1.2-1.1H14v2.2z"
        fill="white"
      />
    </svg>
  );
}

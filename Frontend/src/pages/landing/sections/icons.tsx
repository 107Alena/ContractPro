// Простые SVG-иконки для FeatureCard. Не тянем lucide-react/heroicons ради 6 картинок —
// это снижает bundle landing-chunk. Все иконки 24x24, currentColor, aria-hidden=true.
import type { FeatureIconId } from '../content';

type IconComponent = () => JSX.Element;

function ScanIcon(): JSX.Element {
  return (
    <svg
      viewBox="0 0 24 24"
      width="24"
      height="24"
      fill="none"
      aria-hidden="true"
      focusable="false"
    >
      <path
        d="M4 7V5a1 1 0 0 1 1-1h2M4 17v2a1 1 0 0 0 1 1h2M20 7V5a1 1 0 0 0-1-1h-2M20 17v2a1 1 0 0 1-1 1h-2M3 12h18"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
    </svg>
  );
}

function RiskIcon(): JSX.Element {
  return (
    <svg
      viewBox="0 0 24 24"
      width="24"
      height="24"
      fill="none"
      aria-hidden="true"
      focusable="false"
    >
      <path
        d="M12 3 3 20h18L12 3Zm0 6v5m0 3v.01"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function RecommendIcon(): JSX.Element {
  return (
    <svg
      viewBox="0 0 24 24"
      width="24"
      height="24"
      fill="none"
      aria-hidden="true"
      focusable="false"
    >
      <path
        d="M12 3v2m6.364 1.636-1.414 1.414M21 12h-2M6.636 6.636 5.222 5.222M5 12H3M12 17a5 5 0 1 0-3-9 5 5 0 0 0-1 9v1a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1v-1ZM10 21h4"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function SummaryIcon(): JSX.Element {
  return (
    <svg
      viewBox="0 0 24 24"
      width="24"
      height="24"
      fill="none"
      aria-hidden="true"
      focusable="false"
    >
      <path
        d="M7 4h10a2 2 0 0 1 2 2v14l-4-2-3 2-3-2-4 2V6a2 2 0 0 1 2-2Zm2 5h6m-6 4h6m-6 4h4"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function HistoryIcon(): JSX.Element {
  return (
    <svg
      viewBox="0 0 24 24"
      width="24"
      height="24"
      fill="none"
      aria-hidden="true"
      focusable="false"
    >
      <path
        d="M12 8v4l3 2M4 12a8 8 0 1 0 2.343-5.657M3 4v4h4"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function ShieldIcon(): JSX.Element {
  return (
    <svg
      viewBox="0 0 24 24"
      width="24"
      height="24"
      fill="none"
      aria-hidden="true"
      focusable="false"
    >
      <path
        d="M12 3 4 6v6c0 4.5 3.2 8.5 8 9 4.8-.5 8-4.5 8-9V6l-8-3Zm-3 9 2 2 4-4"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

const ICONS: Record<FeatureIconId, IconComponent> = {
  scan: ScanIcon,
  risk: RiskIcon,
  recommend: RecommendIcon,
  summary: SummaryIcon,
  history: HistoryIcon,
  shield: ShieldIcon,
};

export function FeatureIcon({ id }: { id: FeatureIconId }): JSX.Element {
  const Icon = ICONS[id];
  return <Icon />;
}

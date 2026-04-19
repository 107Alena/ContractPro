// source: Figma fileKey Lxhk7jQyXL3iuoTpiOHxcb — hex'и продублированы в src/app/styles/tokens.css.
// Любое изменение palette/radii/spacing/shadows — синхронно с tokens.css.
// См. high-architecture.md §8.2 + ADR-FE-09 (token pipeline).
import type { Config } from 'tailwindcss';

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        brand: {
          50: 'var(--color-brand-50)',
          500: 'var(--color-brand-500)',
          600: 'var(--color-brand-600)',
        },
        risk: {
          high: 'var(--color-risk-high)',
          medium: 'var(--color-risk-medium)',
          low: 'var(--color-risk-low)',
        },
        fg: {
          DEFAULT: 'var(--color-fg)',
          muted: 'var(--color-fg-muted)',
        },
        bg: {
          DEFAULT: 'var(--color-bg)',
          muted: 'var(--color-bg-muted)',
        },
        border: 'var(--color-border)',
        success: 'var(--color-success)',
        warning: 'var(--color-warning)',
        danger: 'var(--color-danger)',
      },
      fontFamily: {
        sans: ['var(--font-sans)'],
      },
      borderRadius: {
        sm: 'var(--radius-sm)',
        md: 'var(--radius-md)',
        lg: 'var(--radius-lg)',
      },
      boxShadow: {
        sm: 'var(--shadow-sm)',
        md: 'var(--shadow-md)',
        lg: 'var(--shadow-lg)',
      },
      spacing: {
        1: 'var(--space-1)',
        2: 'var(--space-2)',
        3: 'var(--space-3)',
        4: 'var(--space-4)',
        5: 'var(--space-5)',
        6: 'var(--space-6)',
        8: 'var(--space-8)',
        10: 'var(--space-10)',
        12: 'var(--space-12)',
      },
      ringColor: {
        DEFAULT: 'var(--focus-ring-color)',
      },
      ringWidth: {
        DEFAULT: 'var(--focus-ring-width)',
      },
      ringOffsetWidth: {
        DEFAULT: 'var(--focus-ring-offset)',
      },
      zIndex: {
        modal: 'var(--z-modal)',
        popover: 'var(--z-popover)',
        tooltip: 'var(--z-tooltip)',
        toast: 'var(--z-toast)',
      },
      keyframes: {
        'accordion-down': {
          from: { height: '0' },
          to: { height: 'var(--radix-accordion-content-height)' },
        },
        'accordion-up': {
          from: { height: 'var(--radix-accordion-content-height)' },
          to: { height: '0' },
        },
      },
      animation: {
        'accordion-down': 'accordion-down 200ms ease-out',
        'accordion-up': 'accordion-up 200ms ease-out',
      },
    },
  },
  plugins: [],
} satisfies Config;

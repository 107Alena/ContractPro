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
          'high-bg': 'var(--color-risk-high-bg)',
          'high-bg-soft': 'var(--color-risk-high-bg-soft)',
          'medium-bg': 'var(--color-risk-medium-bg)',
          'low-bg': 'var(--color-risk-low-bg)',
        },
        fg: {
          DEFAULT: 'var(--color-fg)',
          muted: 'var(--color-fg-muted)',
          subtle: 'var(--color-fg-subtle)',
          disabled: 'var(--color-fg-disabled)',
        },
        bg: {
          DEFAULT: 'var(--color-bg)',
          muted: 'var(--color-bg-muted)',
        },
        border: {
          DEFAULT: 'var(--color-border)',
          subtle: 'var(--color-border-subtle)',
        },
        divider: 'var(--color-divider)',
        success: 'var(--color-success)',
        warning: 'var(--color-warning)',
        danger: 'var(--color-danger)',
        processing: 'var(--color-processing)',
      },
      fontFamily: {
        sans: ['var(--font-sans)'],
      },
      fontSize: {
        11: 'var(--text-11)',
        12: 'var(--text-12)',
        13: 'var(--text-13)',
        14: 'var(--text-14)',
        15: 'var(--text-15)',
        16: 'var(--text-16)',
        20: 'var(--text-20)',
        60: 'var(--text-60)',
      },
      borderRadius: {
        sm: 'var(--radius-sm)',
        md: 'var(--radius-md)',
        lg: 'var(--radius-lg)',
        xl: 'var(--radius-xl)',
        pill: 'var(--radius-pill)',
      },
      boxShadow: {
        sm: 'var(--shadow-sm)',
        md: 'var(--shadow-md)',
        lg: 'var(--shadow-lg)',
        card: 'var(--shadow-card)',
      },
      spacing: {
        1: 'var(--space-1)',
        '1.5': 'var(--space-1-5)',
        2: 'var(--space-2)',
        '2.5': 'var(--space-2-5)',
        3: 'var(--space-3)',
        '3.5': 'var(--space-3-5)',
        4: 'var(--space-4)',
        5: 'var(--space-5)',
        6: 'var(--space-6)',
        7: 'var(--space-7)',
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

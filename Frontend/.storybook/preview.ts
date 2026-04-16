import type { Preview } from '@storybook/react';

import '../src/app/styles/index.css';

const preview: Preview = {
  parameters: {
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
    backgrounds: {
      default: 'surface',
      values: [
        { name: 'surface', value: 'var(--color-bg)' },
        { name: 'muted', value: 'var(--color-bg-muted)' },
      ],
    },
    a11y: {
      // WCAG 2.1 AA — axe-core теги. Блокирующие нарушения фейлят Chromatic-gate
      // и play-функции через @storybook/addon-interactions.
      config: {
        rules: [
          { id: 'color-contrast', enabled: true },
          { id: 'label', enabled: true },
          { id: 'button-name', enabled: true },
          { id: 'aria-valid-attr', enabled: true },
          { id: 'aria-required-attr', enabled: true },
        ],
      },
      options: {
        runOnly: {
          type: 'tag',
          values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'],
        },
      },
    },
    options: {
      storySort: {
        order: ['Welcome', 'Shared', 'Entities', 'Features', 'Widgets', 'Pages'],
      },
    },
  },
};

export default preview;

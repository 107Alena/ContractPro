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
      config: { rules: [] },
    },
    options: {
      storySort: {
        order: ['Welcome', 'Shared', 'Entities', 'Features', 'Widgets', 'Pages'],
      },
    },
  },
};

export default preview;

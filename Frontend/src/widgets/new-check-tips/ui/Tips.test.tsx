// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { Tips } from './Tips';

describe('Tips', () => {
  it('рендерит region с заголовком и 3 советами', () => {
    render(<Tips />);
    expect(screen.getByRole('region', { name: 'Советы для лучшего результата' })).toBeDefined();
    expect(screen.getAllByRole('listitem')).toHaveLength(3);
  });
});

// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { TrustFooter } from './TrustFooter';

afterEach(cleanup);

describe('TrustFooter', () => {
  it('рендерит четыре trust-маркера', () => {
    render(<TrustFooter />);
    expect(screen.getByText(/Данные зашифрованы/)).toBeDefined();
    expect(screen.getByText(/Юрисдикция РФ/)).toBeDefined();
    expect(screen.getByText(/Рекомендательный характер/)).toBeDefined();
    expect(screen.getByText(/Доступ ограничен ролью/)).toBeDefined();
  });
});

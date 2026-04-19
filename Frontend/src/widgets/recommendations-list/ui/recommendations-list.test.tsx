// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { RecommendationsList } from './recommendations-list';

afterEach(cleanup);

describe('RecommendationsList', () => {
  it('empty state — без данных рендерит плейсхолдер', () => {
    render(<RecommendationsList />);
    expect(screen.getByTestId('recommendations-list-empty')).toBeDefined();
  });

  it('error state — рендерит role=alert', () => {
    render(<RecommendationsList error={new Error('boom')} />);
    expect(screen.getByRole('alert')).toBeDefined();
  });

  it('loading state — рендерит spinner', () => {
    render(<RecommendationsList isLoading />);
    expect(screen.getByTestId('recommendations-list-loading')).toBeDefined();
  });

  it('рендерит список рекомендаций с «Было» / «Рекомендуем»', () => {
    render(
      <RecommendationsList
        items={[
          {
            risk_id: 'r1',
            original_text: 'Штраф 10%.',
            recommended_text: 'Штраф не более 5%.',
            explanation: 'Соответствует НК РФ.',
          },
        ]}
      />,
    );
    expect(screen.getByTestId('recommendations-list')).toBeDefined();
    expect(screen.getByText(/Штраф 10%/)).toBeDefined();
    expect(screen.getByText(/Штраф не более 5%/)).toBeDefined();
    expect(screen.getByText(/НК РФ/)).toBeDefined();
  });
});

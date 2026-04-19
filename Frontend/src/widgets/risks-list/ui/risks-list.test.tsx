// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { RisksList } from './risks-list';

afterEach(cleanup);

describe('RisksList', () => {
  it('empty state — без данных рендерит плейсхолдер', () => {
    render(<RisksList />);
    expect(screen.getByTestId('risks-list-empty')).toBeDefined();
  });

  it('error state — рендерит role=alert', () => {
    render(<RisksList error={new Error('boom')} />);
    expect(screen.getByRole('alert')).toBeDefined();
  });

  it('loading state — рендерит spinner', () => {
    render(<RisksList isLoading />);
    expect(screen.getByTestId('risks-list-loading')).toBeDefined();
  });

  it('показывает список рисков с RiskBadge и описанием', () => {
    render(
      <RisksList
        risks={[
          {
            id: 'r1',
            level: 'high',
            description: 'Штраф 10% без ограничений.',
            clause_ref: '5.3',
            legal_basis: 'ГК РФ ст. 330',
          },
        ]}
      />,
    );
    expect(screen.getByTestId('risks-list')).toBeDefined();
    expect(screen.getByText(/Штраф 10%/)).toBeDefined();
    expect(screen.getByText(/5\.3/)).toBeDefined();
    expect(screen.getByText(/ГК РФ/)).toBeDefined();
  });
});

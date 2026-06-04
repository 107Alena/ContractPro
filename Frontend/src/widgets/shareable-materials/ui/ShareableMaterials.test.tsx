// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { ShareableMaterials } from './ShareableMaterials';

afterEach(cleanup);

describe('ShareableMaterials', () => {
  it('рендерит секцию и 4 карточки материалов', () => {
    render(<ShareableMaterials />);
    expect(screen.getByTestId('shareable-materials')).toBeInTheDocument();
    expect(screen.getByText('Краткая выжимка')).toBeInTheDocument();
    expect(screen.getByText('Полный отчёт проверки')).toBeInTheDocument();
    expect(screen.getByText('Отчёт по различиям')).toBeInTheDocument();
    expect(screen.getByText('Защищённая ссылка')).toBeInTheDocument();
  });
});

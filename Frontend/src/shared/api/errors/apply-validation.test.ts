// Тесты applyValidationErrors (§20.4a): маппинг полей, unmatched, focus,
// optional translator, non-VALIDATION и non-Orchestrator cases.
import { describe, expect, it, type Mock, vi } from 'vitest';

import {
  applyValidationErrors,
  isValidationError,
  type UseFormSetErrorLike,
  type ValidationFieldError,
} from './apply-validation';
import { OrchestratorError } from './orchestrator-error';

interface FormShape extends Record<string, unknown> {
  title: string;
  email: string;
  'parties.0.name': string;
}

// vitest 1.x: `vi.fn<TArgs, TReturn>()` принимает (args-tuple, return). Чтобы
// пользоваться типом `UseFormSetErrorLike` и одновременно Mock-свойствами —
// создаём нетипизированный mock и даём ему два псевдонима.
function makeSetErrorMock<T extends Record<string, unknown> = FormShape>(): {
  setError: UseFormSetErrorLike<T>;
  mock: Mock;
} {
  const mock = vi.fn();
  return { setError: mock as unknown as UseFormSetErrorLike<T>, mock };
}

function makeValidationError(fields: ValidationFieldError[]): OrchestratorError {
  return new OrchestratorError({
    error_code: 'VALIDATION_ERROR',
    message: 'Проверьте введённые данные',
    status: 400,
    details: { fields } as unknown as OrchestratorError['details'],
  });
}

describe('applyValidationErrors — happy path', () => {
  it('маппит все поля, focus — только на первое', () => {
    const { setError, mock } = makeSetErrorMock<FormShape>();
    const err = makeValidationError([
      { field: 'title', code: 'REQUIRED', message: 'Поле обязательно' },
      {
        field: 'email',
        code: 'INVALID_FORMAT',
        message: 'Неверный формат',
        params: { regex: 'email' },
      },
    ]);

    const result = applyValidationErrors<FormShape>(err, setError);
    expect(result).toEqual({ matched: 2, unmatched: [] });
    expect(mock).toHaveBeenCalledTimes(2);
    expect(mock).toHaveBeenNthCalledWith(
      1,
      'title',
      { type: 'REQUIRED', message: 'Поле обязательно' },
      { shouldFocus: true },
    );
    expect(mock).toHaveBeenNthCalledWith(
      2,
      'email',
      { type: 'INVALID_FORMAT', message: 'Неверный формат' },
      { shouldFocus: false },
    );
  });

  it('обрабатывает nested field paths (parties.0.name — RHF-совместимо)', () => {
    const { setError, mock } = makeSetErrorMock<FormShape>();
    const err = makeValidationError([
      { field: 'parties.0.name', code: 'TOO_SHORT', message: 'Минимум 2 символа' },
    ]);
    applyValidationErrors<FormShape>(err, setError);
    expect(mock).toHaveBeenCalledWith(
      'parties.0.name',
      { type: 'TOO_SHORT', message: 'Минимум 2 символа' },
      { shouldFocus: true },
    );
  });

  it('пустые details.fields → {0, []} без вызовов setError', () => {
    const { setError, mock } = makeSetErrorMock<FormShape>();
    const err = makeValidationError([]);
    const result = applyValidationErrors<FormShape>(err, setError);
    expect(result).toEqual({ matched: 0, unmatched: [] });
    expect(mock).not.toHaveBeenCalled();
  });

  it('details без поля fields → {0, []}', () => {
    const err = new OrchestratorError({
      error_code: 'VALIDATION_ERROR',
      message: 'x',
      details: { other: 'junk' } as unknown as OrchestratorError['details'],
    });
    const { setError } = makeSetErrorMock<FormShape>();
    const result = applyValidationErrors<FormShape>(err, setError);
    expect(result).toEqual({ matched: 0, unmatched: [] });
  });
});

describe('applyValidationErrors — unmatched', () => {
  it('поля, не существующие в форме (setError throws), складываются в unmatched', () => {
    const mock = vi
      .fn()
      .mockImplementationOnce(() => undefined)
      .mockImplementationOnce(() => {
        throw new Error('unknown field in form');
      });
    const setError = mock as unknown as UseFormSetErrorLike<FormShape>;

    const fields: ValidationFieldError[] = [
      { field: 'title', code: 'REQUIRED', message: '1' },
      { field: 'not_in_form', code: 'DUPLICATE', message: '2' },
    ];
    const err = makeValidationError(fields);
    const result = applyValidationErrors<FormShape>(err, setError);

    expect(result.matched).toBe(1);
    expect(result.unmatched).toHaveLength(1);
    expect(result.unmatched[0]?.field).toBe('not_in_form');
  });
});

describe('applyValidationErrors — translator (i18n override)', () => {
  it('translate(code, fallback, params) используется вместо серверного message', () => {
    const { setError, mock } = makeSetErrorMock<FormShape>();
    const err = makeValidationError([
      { field: 'title', code: 'TOO_LONG', message: 'Слишком длинное', params: { max: 100 } },
    ]);

    const translate = vi.fn(
      (code: string, _fallback: string, params: Record<string, unknown>): string =>
        `i18n.${code}(max=${String(params.max)})`,
    );

    applyValidationErrors<FormShape>(err, setError, translate);
    expect(translate).toHaveBeenCalledWith('TOO_LONG', 'Слишком длинное', { max: 100 });
    expect(mock).toHaveBeenCalledWith(
      'title',
      { type: 'TOO_LONG', message: 'i18n.TOO_LONG(max=100)' },
      expect.any(Object),
    );
  });

  it('translate получает пустой объект params, если backend не прислал params', () => {
    const { setError } = makeSetErrorMock<FormShape>();
    const err = makeValidationError([{ field: 'title', code: 'REQUIRED', message: 'fb' }]);
    const translate = vi.fn(() => 'x');
    applyValidationErrors<FormShape>(err, setError, translate);
    expect(translate).toHaveBeenCalledWith('REQUIRED', 'fb', {});
  });
});

describe('applyValidationErrors — noop cases', () => {
  it('не-OrchestratorError → noop', () => {
    const { setError, mock } = makeSetErrorMock<FormShape>();
    expect(applyValidationErrors(new Error('x'), setError)).toEqual({ matched: 0, unmatched: [] });
    expect(mock).not.toHaveBeenCalled();
  });

  it('Orchestrator без кода VALIDATION_ERROR → noop', () => {
    const { setError, mock } = makeSetErrorMock<FormShape>();
    const err = new OrchestratorError({ error_code: 'FILE_TOO_LARGE', message: '..' });
    expect(applyValidationErrors(err, setError)).toEqual({ matched: 0, unmatched: [] });
    expect(mock).not.toHaveBeenCalled();
  });

  it('null / undefined / primitives → noop без throw', () => {
    const { setError } = makeSetErrorMock<FormShape>();
    expect(applyValidationErrors(null, setError)).toEqual({ matched: 0, unmatched: [] });
    expect(applyValidationErrors(undefined, setError)).toEqual({ matched: 0, unmatched: [] });
    expect(applyValidationErrors('string', setError)).toEqual({ matched: 0, unmatched: [] });
  });
});

describe('isValidationError', () => {
  it('true для VALIDATION_ERROR OrchestratorError', () => {
    const err = new OrchestratorError({ error_code: 'VALIDATION_ERROR', message: 'x' });
    expect(isValidationError(err)).toBe(true);
  });

  it('false для других OrchestratorError', () => {
    const err = new OrchestratorError({ error_code: 'INTERNAL_ERROR', message: 'x' });
    expect(isValidationError(err)).toBe(false);
  });

  it('false для не-OrchestratorError', () => {
    expect(isValidationError(new Error('x'))).toBe(false);
    expect(isValidationError(null)).toBe(false);
    expect(isValidationError({ error_code: 'VALIDATION_ERROR' })).toBe(false);
  });
});

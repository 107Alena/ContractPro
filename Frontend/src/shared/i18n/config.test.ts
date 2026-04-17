import { describe, expect, it } from 'vitest';

import { DEFAULT_LOCALE, DEFAULT_NAMESPACE, i18n, I18N_RESOURCES, NAMESPACES } from './config';

describe('i18n config', () => {
  it('инициализируется с русской локалью и namespace common', () => {
    expect(DEFAULT_LOCALE).toBe('ru');
    expect(DEFAULT_NAMESPACE).toBe('common');
    expect(NAMESPACES).toEqual(['common', 'errors']);
  });

  it('экспортирует ресурсы ru.common и ru.errors', () => {
    expect(I18N_RESOURCES.ru.common).toBeDefined();
    expect(I18N_RESOURCES.ru.errors).toBeDefined();
  });

  it('i18n.isInitialized === true после импорта модуля', () => {
    expect(i18n.isInitialized).toBe(true);
    expect(i18n.language).toBe('ru');
  });

  it('t("hello") возвращает значение из ru/common.json (test_step #3)', () => {
    expect(i18n.t('hello')).toBe('Здравствуйте');
  });

  it('t с namespace "errors" возвращает ключи ошибок', () => {
    expect(i18n.t('forbidden.title', { ns: 'errors' })).toBe('Недостаточно прав');
    expect(i18n.t('notFound.code', { ns: 'errors' })).toBe('404');
    expect(i18n.t('serverError.title', { ns: 'errors' })).toBe('Временные проблемы');
    expect(i18n.t('offline.title', { ns: 'errors' })).toBe('Нет соединения');
  });

  it('common:actions содержит reload/retry/home', () => {
    expect(i18n.t('actions.reload', { ns: 'common' })).toBe('Обновить страницу');
    expect(i18n.t('actions.retry', { ns: 'common' })).toBe('Повторить');
    expect(i18n.t('actions.home', { ns: 'common' })).toBe('На главную');
  });
});

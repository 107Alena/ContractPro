import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';

import commonRu from './locales/ru/common.json';
import errorsRu from './locales/ru/errors.json';

export const DEFAULT_LOCALE = 'ru';
export const DEFAULT_NAMESPACE = 'common';
export const NAMESPACES = ['common', 'errors'] as const;

export const I18N_RESOURCES = {
  ru: {
    common: commonRu,
    errors: errorsRu,
  },
} as const;

void i18n.use(initReactI18next).init({
  resources: I18N_RESOURCES,
  lng: DEFAULT_LOCALE,
  fallbackLng: DEFAULT_LOCALE,
  defaultNS: DEFAULT_NAMESPACE,
  ns: NAMESPACES as unknown as string[],
  interpolation: { escapeValue: false },
  react: { useSuspense: false },
  returnNull: false,
});

export { i18n };

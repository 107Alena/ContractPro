// Доменные типы feature share-link.
//
// FSD-граница: `features/*` не могут импортировать друг у друга (см.
// eslint.config `boundaries/element-types`). Типы export-формата дублируются
// здесь; их набор тривиален и задан API-контрактом (§7.6 api-specification).

export type ShareLinkFormat = 'pdf' | 'docx';

export interface ShareLinkInput {
  contractId: string;
  versionId: string;
  format: ShareLinkFormat;
}

export interface ShareLinkResult {
  /** Защищённый presigned URL (TTL 5 мин). */
  location: string;
}

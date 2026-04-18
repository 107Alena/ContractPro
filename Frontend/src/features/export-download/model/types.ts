// Доменные типы feature export-download.
//
// Endpoint (§7.6 api-specification, UR-10):
//   GET /contracts/{contract_id}/versions/{version_id}/export/{format}
//     302: Location: <presigned URL, TTL 5 мин>
//     403: PERMISSION_DENIED — экспорт запрещён политикой организации
//          (для BUSINESS_USER — см. useCanExport §5.6)
//     404: отчёт ещё не готов (RESULTS_NOT_READY / ARTIFACT_NOT_FOUND)
//
// UI использует feature двумя путями: download (window.location.assign на
// Location) и share-link (копирование Location в clipboard — см. feature
// `share-link`, дублирующее экспорт для FSD-границ).

export type ExportFormat = 'pdf' | 'docx';

export interface ExportReportInput {
  contractId: string;
  versionId: string;
  format: ExportFormat;
}

export interface ExportLocation {
  /** Подписанный presigned URL с TTL 5 минут. */
  location: string;
}

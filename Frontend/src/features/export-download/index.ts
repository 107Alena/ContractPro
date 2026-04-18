// Barrel: публичный API фичи export-download (§6.1, §17.5 high-architecture).
//
// Импортировать ТОЛЬКО этот путь (FSD-граница). Потребители —
// widgets/export-share-modal (FE-TASK-039), ResultPage (FE-TASK-044),
// ReportsPage (FE-TASK-046).
export type { ExportReportOptions } from './api/export-report';
export { exportReport, exportReportEndpoint } from './api/export-report';
export { isExportNotReadyError } from './lib/is-export-not-ready';
export type { ExportFormat, ExportLocation, ExportReportInput } from './model/types';
export {
  useExportDownload,
  type UseExportDownloadOptions,
  type UseExportDownloadResult,
} from './model/use-export-download';

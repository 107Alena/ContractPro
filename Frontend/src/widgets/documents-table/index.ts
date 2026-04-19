// Публичный API widgets/documents-table (FSD: импортировать ТОЛЬКО этот путь).
export {
  buildDocumentsTableColumns,
  DOCUMENT_STATUS_LABEL,
  type DocumentStatusDisplay,
} from './model/columns';
export {
  DocumentsTable,
  type DocumentsTableProps,
  ROW_HEIGHT_PX,
  VIRTUALIZATION_THRESHOLD,
} from './ui/DocumentsTable';

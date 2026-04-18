// Handler GET /contracts/{id}/versions/{vid}/export/{format}.
// По §7.6 — 302 Redirect на presigned URL (TTL 5 минут).

import { http, HttpResponse } from 'msw';

import { errorResponse, type HandlerBase, joinPath } from './_helpers';

const PRESIGNED_HOSTS: Record<string, string> = {
  pdf: 'https://presigned.example/contractpro/report.pdf?X-Expires=300',
  docx: 'https://presigned.example/contractpro/report.docx?X-Expires=300',
};

export function createExportHandlers(base: HandlerBase) {
  return [
    http.get(
      joinPath(base, '/contracts/:contractId/versions/:versionId/export/:format'),
      ({ params }) => {
        const format = params.format as string;
        const location = PRESIGNED_HOSTS[format];
        if (!location) {
          return errorResponse(400, 'VALIDATION_ERROR', 'Поддерживаются только pdf и docx');
        }
        return new HttpResponse(null, { status: 302, headers: { Location: location } });
      },
    ),
  ];
}

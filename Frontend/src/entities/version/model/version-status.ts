// VersionStatus — публичный алиас для `UserProcessingStatus` (OpenAPI-тип).
//
// Почему алиас, а не переэкспорт напрямую: high-architecture.md §143 явно
// называет `entities/version` владельцем доменного понятия «версия и её
// статус». Алиас делает эту семантику читаемой в потребителях (widgets/pages)
// и даёт точку расширения, если клиентская модель когда-либо разойдётся с
// публичным enum'ом.
import type { UserProcessingStatus } from '@/shared/api';

export type VersionStatus = UserProcessingStatus;

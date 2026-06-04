// Фикстуры результатов анализа: risks / summary / recommendations / полный AnalysisResults.
// Source of truth — структура из §17.5 архитектуры и OpenAPI schemas.

import type { components } from '@/shared/api/openapi';

import { IDS } from './ids';

type Risk = components['schemas']['Risk'];
type RiskList = components['schemas']['RiskList'];
type RiskProfile = components['schemas']['RiskProfile'];
type Recommendation = components['schemas']['Recommendation'];
type RecommendationList = components['schemas']['RecommendationList'];
type ContractSummaryResult = components['schemas']['ContractSummaryResult'];
type AnalysisResults = components['schemas']['AnalysisResults'];

export const riskProfile: RiskProfile = {
  overall_level: 'medium',
  high_count: 2,
  medium_count: 5,
  low_count: 3,
};

export const risks: Risk[] = [
  {
    id: 'risk-1',
    level: 'high',
    description: 'Не указан предельный срок ответственности исполнителя за просрочку.',
    clause_ref: 'Пункт 4.2',
    legal_basis: 'ст. 330 ГК РФ',
  },
  {
    id: 'risk-2',
    level: 'high',
    description: 'Односторонний отказ заказчика от договора без компенсации расходов.',
    clause_ref: 'Пункт 7.1',
    legal_basis: 'ст. 782 ГК РФ',
  },
  {
    id: 'risk-3',
    level: 'medium',
    description: 'Отсутствует порядок урегулирования споров (претензионный порядок).',
    clause_ref: 'Раздел 9',
    legal_basis: 'ст. 4 АПК РФ',
  },
  {
    id: 'risk-4',
    level: 'low',
    description: 'Нет ссылки на приложения в теле договора.',
    clause_ref: 'Раздел 1',
    legal_basis: 'Рекомендация',
  },
];

export const recommendations: Recommendation[] = [
  {
    risk_id: 'risk-1',
    original_text: 'Исполнитель несёт ответственность за нарушение сроков.',
    recommended_text:
      'Исполнитель несёт ответственность в размере 0,1% от стоимости услуг за каждый день просрочки, но не более 10% от общей стоимости.',
    explanation:
      'Ограничение размера неустойки предотвращает злоупотребление правом (ст. 330 ГК РФ).',
  },
  {
    risk_id: 'risk-2',
    original_text: 'Заказчик вправе в любой момент отказаться от договора.',
    recommended_text:
      'Заказчик вправе отказаться от договора с компенсацией исполнителю фактически понесённых расходов (ст. 782 ГК РФ).',
    explanation: 'Защита интересов исполнителя при немотивированном отказе заказчика.',
  },
  {
    risk_id: 'risk-3',
    original_text: '',
    recommended_text:
      'Споры разрешаются путём переговоров. Претензионный срок ответа — 30 календарных дней. При недостижении согласия — в Арбитражном суде по месту нахождения ответчика.',
    explanation: 'Соблюдение обязательного претензионного порядка.',
  },
];

export const summary: ContractSummaryResult = {
  summary:
    'Договор оказания услуг на сумму 1,5 млн руб. с ООО «Альфа». Срок действия — 12 месяцев. Основные риски: отсутствие ограничения неустойки и несбалансированные условия расторжения.',
  aggregate_score: {
    score: 0.65,
    label: 'Средний риск',
  },
  key_parameters: {
    parties: ['ООО «Контракт-Сервис»', 'ООО «Альфа»'],
    subject: 'Оказание консалтинговых услуг',
    price: '1 500 000 руб.',
    duration: '12 месяцев',
    penalties: 'Не ограничены',
    jurisdiction: 'г. Москва',
  },
};

export const riskList: RiskList = {
  risks,
  risk_profile: riskProfile,
};

// Демо-дельта рисков для сравнения версий (delta v1 → v2). База «опаснее»;
// в target два риска устранены (d-high-1, d-med-2), один новый (d-med-3) — даёт
// наглядную ненулевую дельту в dev:e2e на /contracts/<delta>/compare?base=v1&target=v2.
export const risksDeltaV1: RiskList = {
  risk_profile: { overall_level: 'high', high_count: 1, medium_count: 2, low_count: 1 },
  risks: [
    {
      id: 'd-high-1',
      level: 'high',
      description: 'Неустойка предусмотрена только для заказчика (асимметрия).',
      clause_ref: 'Пункт 7.2',
      legal_basis: 'ст. 330 ГК РФ',
    },
    {
      id: 'd-med-1',
      level: 'medium',
      description: 'Не определён порядок приёмки работ.',
      clause_ref: 'Пункт 5.1',
    },
    {
      id: 'd-med-2',
      level: 'medium',
      description: 'Размытые сроки оплаты этапов.',
      clause_ref: 'Пункт 3.4',
    },
    {
      id: 'd-low-1',
      level: 'low',
      description: 'Нет ссылки на приложение №2 в теле договора.',
      clause_ref: 'Пункт 1.3',
    },
  ],
};

export const risksDeltaV2: RiskList = {
  risk_profile: { overall_level: 'medium', high_count: 0, medium_count: 2, low_count: 1 },
  risks: [
    {
      id: 'd-med-1',
      level: 'medium',
      description: 'Не определён порядок приёмки работ.',
      clause_ref: 'Пункт 5.1',
    },
    {
      id: 'd-low-1',
      level: 'low',
      description: 'Нет ссылки на приложение №2 в теле договора.',
      clause_ref: 'Пункт 1.3',
    },
    {
      id: 'd-med-3',
      level: 'medium',
      description: 'Неуточнённый промежуточный платёж в новой редакции.',
      clause_ref: 'Пункт 3.5',
    },
  ],
};

// Карта versionId → RiskList для per-version /risks. Версии без записи получают
// дефолтный riskList (Dashboard/ContractDetail KeyRisks не затронуты).
export const risksByVersionId: Record<string, RiskList> = {
  [IDS.versions.deltaV1]: risksDeltaV1,
  [IDS.versions.deltaV2]: risksDeltaV2,
};

export const recommendationList: RecommendationList = {
  items: recommendations,
};

export const analysisResults: AnalysisResults = {
  version_id: IDS.versions.alphaV1,
  status: 'READY',
  contract_type: { contract_type: 'услуги', confidence: 0.92 },
  ...(summary.key_parameters !== undefined && { key_parameters: summary.key_parameters }),
  risk_profile: riskProfile,
  risks,
  recommendations,
  ...(summary.summary !== undefined && { summary: summary.summary }),
  ...(summary.aggregate_score !== undefined && { aggregate_score: summary.aggregate_score }),
};

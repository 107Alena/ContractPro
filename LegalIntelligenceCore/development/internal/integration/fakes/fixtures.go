package fakes

import (
	"encoding/json"
	"fmt"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// Test data: realistic Russian-language artifact fixtures that LIC-TASK-049
// (happy-path INITIAL pipeline) and 050/051 plug into FakeDM. The values
// match the DM-side wire shape (byte-for-byte json.RawMessage; LIC does
// not own the typed structs — those are DM's, see
// DocumentProcessing/development/internal/domain/model/). The fixtures are
// MINIMAL but VALID against the LIC agent-input expectations and — for the
// canned agent responses below — schema-valid against the corresponding
// JSON schemas embedded by internal/agents/schemas. fixtures_test.go pins
// each canned response against its schema via gojsonschema so the
// production Schema Validator (LIC-TASK-023) and BaseAgent (LIC-TASK-024)
// will accept them at consumer-task time without a single tweak.

// SemanticTreeRU — small contract-shaped SEMANTIC_TREE for a fictional
// Russian-language supply contract ("Договор поставки № 12 от 15.05.2024").
const SemanticTreeRU = `{
  "version": "v1",
  "root": {
    "id": "root",
    "node_type": "DOCUMENT",
    "title": "Договор поставки № 12",
    "children": [
      {
        "id": "preamble",
        "node_type": "PREAMBLE",
        "text": "Общество с ограниченной ответственностью \"Альфа\" (Поставщик) и Общество с ограниченной ответственностью \"Бета\" (Покупатель) заключили настоящий Договор о нижеследующем."
      },
      {
        "id": "s1",
        "node_type": "SECTION",
        "title": "1. Предмет договора",
        "children": [
          {
            "id": "c1.1",
            "node_type": "CLAUSE",
            "number": "1.1",
            "text": "Поставщик обязуется передать в собственность Покупателя товар согласно Спецификации (Приложение № 1)."
          }
        ]
      },
      {
        "id": "s2",
        "node_type": "SECTION",
        "title": "2. Цена и порядок расчетов",
        "children": [
          {
            "id": "c2.1",
            "node_type": "CLAUSE",
            "number": "2.1",
            "text": "Общая сумма Договора составляет 1 200 000 (один миллион двести тысяч) рублей 00 копеек, включая НДС 20%."
          },
          {
            "id": "c2.2",
            "node_type": "CLAUSE",
            "number": "2.2",
            "text": "Оплата производится в течение 10 (десяти) рабочих дней с момента подписания товарной накладной."
          }
        ]
      },
      {
        "id": "s3",
        "node_type": "SECTION",
        "title": "3. Ответственность сторон",
        "children": [
          {
            "id": "c3.1",
            "node_type": "CLAUSE",
            "number": "3.1",
            "text": "За нарушение сроков поставки Поставщик уплачивает Покупателю неустойку в размере 0,1% от стоимости непоставленного товара за каждый день просрочки."
          }
        ]
      }
    ]
  }
}`

// ExtractedTextRU — matching EXTRACTED_TEXT artifact in the DP-faithful
// wire shape (DocumentProcessing/development/internal/domain/model/
// document.go: ExtractedText{document_id, pages:[{page_number, text}]}).
// internal/agents/artifacts.ExtractedText / typeclassifier's local
// extractedText decoder both read the per-page text via this shape; the
// alternative single-string shape used by an earlier LIC-TASK-048 draft
// decoded to zero pages and failed Spec.Parts with an "empty text" build
// defect (LIC-TASK-049 forensic finding — corrected here so the canned
// fixture matches the schema every consumer expects).
const ExtractedTextRU = `{
  "document_id": "00000000-0000-0000-0000-000000000000",
  "pages": [
    {
      "page_number": 1,
      "text": "Договор поставки № 12\nг. Москва\n15 мая 2024 г.\n\nОбщество с ограниченной ответственностью \"Альфа\" (далее — Поставщик), в лице Генерального директора Иванова И. И., с одной стороны, и Общество с ограниченной ответственностью \"Бета\" (далее — Покупатель), в лице Директора Петрова П. П., с другой стороны, заключили настоящий Договор о нижеследующем."
    },
    {
      "page_number": 2,
      "text": "1. Предмет договора\n1.1. Поставщик обязуется передать в собственность Покупателя товар согласно Спецификации (Приложение № 1).\n\n2. Цена и порядок расчетов\n2.1. Общая сумма Договора составляет 1 200 000 (один миллион двести тысяч) рублей 00 копеек, включая НДС 20%.\n2.2. Оплата производится в течение 10 (десяти) рабочих дней с момента подписания товарной накладной."
    },
    {
      "page_number": 3,
      "text": "3. Ответственность сторон\n3.1. За нарушение сроков поставки Поставщик уплачивает Покупателю неустойку в размере 0,1% от стоимости непоставленного товара за каждый день просрочки."
    }
  ]
}`

// DocumentStructureRU — DOCUMENT_STRUCTURE: three sections, three
// clauses, two parties.
const DocumentStructureRU = `{
  "version": "v1",
  "sections": [
    {"id": "s1", "number": "1", "title": "Предмет договора"},
    {"id": "s2", "number": "2", "title": "Цена и порядок расчетов"},
    {"id": "s3", "number": "3", "title": "Ответственность сторон"}
  ],
  "clauses": [
    {"id": "c1.1", "section_id": "s1", "number": "1.1"},
    {"id": "c2.1", "section_id": "s2", "number": "2.1"},
    {"id": "c2.2", "section_id": "s2", "number": "2.2"},
    {"id": "c3.1", "section_id": "s3", "number": "3.1"}
  ],
  "appendices": [],
  "party_details": [
    {"role": "Поставщик", "name": "ООО \"Альфа\"", "representative": "Иванов И. И.", "position": "Генеральный директор"},
    {"role": "Покупатель", "name": "ООО \"Бета\"", "representative": "Петров П. П.", "position": "Директор"}
  ]
}`

// ProcessingWarningsRU — PROCESSING_WARNINGS, empty list (no DP warnings).
const ProcessingWarningsRU = `[]`

// ParentRiskAnalysisRU — minimal RISK_ANALYSIS for RE_CHECK
// (LIC-TASK-051). Shape matches the risk_detection.json schema (same
// schema is reused for the persisted RISK_ANALYSIS artifact).
const ParentRiskAnalysisRU = `{
  "risks": [
    {
      "id": "R-001",
      "level": "medium",
      "description": "Срок оплаты в 10 рабочих дней.",
      "clause_ref": "2.2",
      "legal_basis": "ст. 486 ГК РФ"
    }
  ]
}`

// DefaultArtifactsBundle returns the four mandatory artifact blobs used
// by every INITIAL pipeline.
func DefaultArtifactsBundle() map[model.ArtifactType]json.RawMessage {
	return map[model.ArtifactType]json.RawMessage{
		model.ArtifactSemanticTree:       json.RawMessage(SemanticTreeRU),
		model.ArtifactExtractedText:      json.RawMessage(ExtractedTextRU),
		model.ArtifactDocumentStructure:  json.RawMessage(DocumentStructureRU),
		model.ArtifactProcessingWarnings: json.RawMessage(ProcessingWarningsRU),
	}
}

// ReCheckArtifactsBundle returns DefaultArtifactsBundle + the parent's
// RISK_ANALYSIS (RE_CHECK only).
func ReCheckArtifactsBundle() map[model.ArtifactType]json.RawMessage {
	out := DefaultArtifactsBundle()
	out[model.ArtifactRiskAnalysis] = json.RawMessage(ParentRiskAnalysisRU)
	return out
}

// ----------------------------------------------------------------------------
// Canned agent responses.
//
// SHAPE NOTE: each response is schema-valid against the matching
// internal/agents/schemas/<basename>.json (fixtures_test.go pins this via
// gojsonschema). Tests that need alternative content build their own
// fixtures and pass them through FakeLLMProvider.SetResponseJSON; the
// Schema Validator (LIC-TASK-023) gates them the same way it gates a
// real provider output.
// ----------------------------------------------------------------------------

// ClassifierResponse — AGENT_TYPE_CLASSIFIER. Picks SUPPLY with high
// confidence (above the default LIC_CONFIDENCE_THRESHOLD). Note the
// 12-value contract_type enum — SUPPLY (not "SUPPLY_CONTRACT").
const ClassifierResponse = `{
  "contract_type": "SUPPLY",
  "confidence": 0.92,
  "alternatives": [
    {"contract_type": "SERVICES", "confidence": 0.05},
    {"contract_type": "OTHER",    "confidence": 0.03}
  ],
  "rationale": "Документ озаглавлен 'Договор поставки', содержит обязательства Поставщика передать товар в собственность Покупателя.",
  "prompt_injection_detected": false
}`

// ClassifierLowConfidenceResponse — same agent, low confidence (below
// the default threshold of 0.75). Drives the pause + classification-
// uncertain flow (050).
const ClassifierLowConfidenceResponse = `{
  "contract_type": "OTHER",
  "confidence": 0.55,
  "alternatives": [
    {"contract_type": "SUPPLY",   "confidence": 0.30},
    {"contract_type": "SERVICES", "confidence": 0.15}
  ],
  "rationale": "Документ нестандартной структуры; вид договора однозначно не определён.",
  "prompt_injection_detected": false
}`

// KeyParamsResponse — AGENT_KEY_PARAMS. Required: parties (array of
// strings), subject, price, duration, penalties, jurisdiction
// (string|null each).
const KeyParamsResponse = `{
  "parties": ["ООО \"Альфа\" (Поставщик)", "ООО \"Бета\" (Покупатель)"],
  "subject": "Поставка товара согласно Спецификации (Приложение № 1)",
  "price": "1 200 000 руб., включая НДС 20%",
  "duration": null,
  "penalties": "0,1% от стоимости непоставленного товара за каждый день просрочки",
  "jurisdiction": null,
  "internal_extras": {
    "applicable_law": "Гражданский кодекс РФ",
    "termination": null,
    "acceptance_procedure": "Оплата в течение 10 рабочих дней с момента подписания товарной накладной"
  },
  "prompt_injection_detected": false
}`

// PartyConsistencyResponse — AGENT_PARTY_CONSISTENCY. No findings — the
// fixture has two well-formed parties.
const PartyConsistencyResponse = `{
  "findings": [],
  "summary": "Реквизиты сторон согласованы; нарушений не выявлено.",
  "prompt_injection_detected": false
}`

// MandatoryConditionsResponse — AGENT_MANDATORY_CONDITIONS. code must
// match ^MC_[A-Z0-9_]+$; status one of FOUND_OK / FOUND_AMBIGUOUS /
// MISSING.
const MandatoryConditionsResponse = `{
  "contract_type": "SUPPLY",
  "conditions": [
    {"code": "MC_SUBJECT",          "label": "Предмет договора",           "status": "FOUND_OK", "legal_basis": "ст. 506 ГК РФ", "found_in": ["1.1"]},
    {"code": "MC_PRICE",            "label": "Цена",                       "status": "FOUND_OK", "legal_basis": "ст. 485 ГК РФ", "found_in": ["2.1"]},
    {"code": "MC_PAYMENT_TERMS",    "label": "Сроки оплаты",               "status": "FOUND_OK", "legal_basis": "ст. 486 ГК РФ", "found_in": ["2.2"]},
    {"code": "MC_LIABILITY",        "label": "Ответственность сторон",     "status": "FOUND_OK", "legal_basis": "ст. 521 ГК РФ", "found_in": ["3.1"]}
  ],
  "summary": "Все обязательные условия для договора поставки присутствуют.",
  "prompt_injection_detected": false
}`

// RiskDetectionResponse — AGENT_RISK_DETECTION. risks[].id ^R-[0-9]{3,}$,
// level=high/medium/low; legal_basis required.
const RiskDetectionResponse = `{
  "risks": [
    {
      "id": "R-001",
      "level": "medium",
      "description": "Срок оплаты в 10 рабочих дней может вызывать кассовые разрывы для Покупателя при крупной поставке.",
      "clause_ref": "2.2",
      "legal_basis": "ст. 486 ГК РФ — обязанность покупателя оплатить товар, общие условия о сроках оплаты",
      "category": "AMBIGUOUS_ACCEPTANCE",
      "rationale": "internal_only_field"
    }
  ],
  "summary": "Выявлен один риск средней значимости — короткий срок оплаты.",
  "prompt_injection_detected": false
}`

// RecommendationResponse — AGENT_RECOMMENDATION. TOP-LEVEL ARRAY shape
// (recommendation.json declares "type":"array").
const RecommendationResponse = `[
  {
    "risk_id": "R-001",
    "original_text": "Оплата производится в течение 10 (десяти) рабочих дней с момента подписания товарной накладной.",
    "recommended_text": "Оплата производится в течение 20 (двадцати) рабочих дней с момента подписания товарной накладной.",
    "explanation": "Увеличение срока оплаты до 20 рабочих дней снижает риск кассового разрыва Покупателя без ущерба для интересов Поставщика."
  }
]`

// SummaryResponse — AGENT_SUMMARY. Required: text (minLength 200,
// maxLength 3000). Russian text below is well above the 200-char floor.
const SummaryResponse = `{
  "text": "Договор поставки между ООО \"Альфа\" (Поставщик) и ООО \"Бета\" (Покупатель) на общую сумму 1 200 000 рублей с НДС 20%, со сроком оплаты 10 рабочих дней с момента подписания товарной накладной и неустойкой 0,1% за каждый день просрочки поставки. Документ соответствует стандартной структуре договора поставки; все обязательные условия присутствуют. Выявлен один риск средней значимости — короткий срок оплаты, рекомендуется увеличить до 20 рабочих дней."
}`

// DetailedReportResponse — AGENT_DETAILED_REPORT. sections[].section_code
// must be one of {OVERVIEW, KEY_PARAMETERS, PARTY_DATA,
// MANDATORY_CONDITIONS, RISKS, RECOMMENDATIONS_SUMMARY, WARNINGS}; each
// item carries title + content (+ optional severity / clause_ref /
// legal_basis / linked_risk_id / linked_recommendation).
//
// warnings is an object-map; when no prompt injection is detected the
// PROMPT_INJECTION_DETECTED key is OMITTED (the schema forbids
// detected=false because of "const":true on the detected property; the
// Aggregator only emits the entry when detection_count >= 1).
const DetailedReportResponse = `{
  "sections": [
    {
      "section_code": "OVERVIEW",
      "title": "Обзор",
      "items": [
        {"title": "Краткое описание", "content": "Договор поставки соответствует стандартной структуре. Ключевые условия определены явно."}
      ]
    },
    {
      "section_code": "KEY_PARAMETERS",
      "title": "Ключевые параметры",
      "items": [
        {"title": "Сумма",       "content": "1 200 000 руб. (НДС 20%)"},
        {"title": "Срок оплаты", "content": "10 рабочих дней"}
      ]
    },
    {
      "section_code": "RISKS",
      "title": "Риски",
      "items": [
        {
          "title": "Срок оплаты 10 рабочих дней",
          "content": "Срок оплаты в 10 рабочих дней может вызывать кассовые разрывы.",
          "severity": "medium",
          "clause_ref": "2.2",
          "legal_basis": "ст. 486 ГК РФ",
          "linked_risk_id": "R-001"
        }
      ]
    }
  ]
}`

// RiskDeltaResponse — AGENT_RISK_DELTA (RE_CHECK only). Required:
// base_version_id, target_version_id (uuid format), added, removed,
// changed, summary.
const RiskDeltaResponse = `{
  "base_version_id": "00000000-0000-0000-0000-000000000001",
  "target_version_id": "00000000-0000-0000-0000-000000000002",
  "added": [],
  "removed": [],
  "changed": [
    {
      "target_id": "R-001",
      "base_id": "R-001",
      "old_level": "medium",
      "new_level": "high",
      "old_clause_ref": "2.2",
      "new_clause_ref": "2.2",
      "explanation": "Уровень риска повышен из-за изменения срока оплаты в новой версии."
    }
  ],
  "profile_change": {
    "old_overall_level": "medium",
    "new_overall_level": "high",
    "old_high_count": 0, "new_high_count": 1,
    "old_medium_count": 1, "new_medium_count": 0,
    "old_low_count": 0, "new_low_count": 0
  },
  "summary": "Один риск изменил уровень с medium на high; общий профиль изменён с medium на high."
}`

// CannedResponseFor returns the canned JSON for the given agent. Tests
// that want every agent installed at once iterate AllAgentIDs() and call
// FakeLLMProvider.SetResponseJSON(agent, model, CannedResponseFor(agent)).
//
// Unknown agent panics — defensive, the 9-agent set is finite.
func CannedResponseFor(a model.AgentID) string {
	switch a {
	case model.AgentTypeClassifier:
		return ClassifierResponse
	case model.AgentKeyParams:
		return KeyParamsResponse
	case model.AgentPartyConsistency:
		return PartyConsistencyResponse
	case model.AgentMandatoryConditions:
		return MandatoryConditionsResponse
	case model.AgentRiskDetection:
		return RiskDetectionResponse
	case model.AgentRecommendation:
		return RecommendationResponse
	case model.AgentSummary:
		return SummaryResponse
	case model.AgentDetailedReport:
		return DetailedReportResponse
	case model.AgentRiskDelta:
		return RiskDeltaResponse
	default:
		panic(fmt.Sprintf("fakes: no canned response for agent %q", a))
	}
}

// ----------------------------------------------------------------------------
// Builders.
// ----------------------------------------------------------------------------

// BuildAgentInput constructs a model.AgentInput envelope with the IDs
// and DM artifacts populated. Upstream-agent fields stay nil — tests
// chain Run() calls themselves.
func BuildAgentInput(correlationID, jobID, documentID, versionID, organizationID string, artifacts map[model.ArtifactType]json.RawMessage) model.AgentInput {
	cp := make(map[model.ArtifactType]json.RawMessage, len(artifacts))
	for k, v := range artifacts {
		cp[k] = v
	}
	return model.AgentInput{
		CorrelationID:  correlationID,
		JobID:          jobID,
		DocumentID:     documentID,
		VersionID:      versionID,
		OrganizationID: organizationID,
		Artifacts:      cp,
	}
}

// BuildArtifactsResponse wraps an artifact map in an ArtifactsResponse
// (no missing types, no error). Sugar for FakeDM.SetArtifactsResponse.
func BuildArtifactsResponse(artifacts map[model.ArtifactType]json.RawMessage) ArtifactsResponse {
	cp := make(map[model.ArtifactType]json.RawMessage, len(artifacts))
	for k, v := range artifacts {
		cp[k] = v
	}
	return ArtifactsResponse{Artifacts: cp}
}

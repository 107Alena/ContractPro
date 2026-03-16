package structure

import (
	"context"
	"strings"
	"testing"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// --- helpers ---

func textFromString(docID, text string) *model.ExtractedText {
	return &model.ExtractedText{
		DocumentID: docID,
		Pages:      []model.PageText{{PageNumber: 1, Text: text}},
	}
}

func hasWarning(warnings []model.ProcessingWarning, code string) bool {
	for _, w := range warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}

// --- test data ---

const fullContractText = `ДОГОВОР ОКАЗАНИЯ УСЛУГ № 123

1. Предмет договора
1.1. Исполнитель обязуется оказать Заказчику консультационные услуги.
1.1.1. Услуги включают юридическое консультирование.
1.1.2. Услуги включают подготовку документов.
1.2. Заказчик обязуется оплатить услуги в порядке и сроки, установленные настоящим Договором.

2. Стоимость услуг и порядок расчётов
2.1. Стоимость услуг составляет 100 000 (сто тысяч) рублей.
2.2. Оплата производится в течение 5 рабочих дней.

3. Сроки оказания услуг
Договор действует с 01.01.2026 по 31.12.2026.

Приложение 1. Перечень услуг
Детальное описание оказываемых услуг согласно пункту 1.1.

Приложение 2. Форма акта
Форма акта приёмки-передачи оказанных услуг.

Реквизиты сторон:

Заказчик:
ООО "Ромашка"
ИНН: 7701234567
ОГРН: 1027700132195
Адрес: г. Москва, ул. Ленина, д. 1
Генеральный директор: Иванов Иван Иванович

Исполнитель:
ИП Петров П.П.
ИНН: 770987654321
Адрес: г. Москва, ул. Пушкина, д. 2`

const sectionsOnlyText = `ДОГОВОР АРЕНДЫ

1. Предмет договора
Арендодатель передаёт во временное пользование помещение.

2. Сроки аренды
Срок аренды составляет 12 месяцев.

3. Ответственность сторон
Стороны несут ответственность в соответствии с законодательством РФ.`

const partyDetailsText = `Реквизиты сторон:

Продавец:
ЗАО "Альфа"
ИНН: 7707123456
ОГРН: 1037739169335
Юридический адрес: г. Санкт-Петербург, пр. Невский, д. 10
Представитель: Сидоров Сидор Сидорович

Покупатель:
ООО "Бета"
ИНН: 771234567890
ОГРН: 5177746100001
Адрес: г. Казань, ул. Баумана, д. 5
В лице: Козлова Мария Петровна`

const noStructureText = `Это обычный текст без какой-либо структуры.
Он не содержит нумерованных разделов, пунктов или подпунктов.
Просто несколько строк текста для тестирования.`

const multipleAppendicesText = `1. Предмет договора
1.1. Исполнитель оказывает услуги.

Приложение 1. Спецификация товаров
Товар А — 100 шт.
Товар Б — 200 шт.

Приложение 2. График поставок
Январь: Товар А
Февраль: Товар Б

Приложение 3. Форма акта
Акт сверки по итогам месяца.`

// --- tests ---

func TestExtract_FullContract(t *testing.T) {
	ext := NewExtractor()
	ctx := context.Background()
	input := textFromString("doc-full", fullContractText)

	result, warnings, err := ext.Extract(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// DocumentID
	if result.DocumentID != "doc-full" {
		t.Errorf("DocumentID = %q, want %q", result.DocumentID, "doc-full")
	}

	// Sections
	if len(result.Sections) != 3 {
		t.Fatalf("got %d sections, want 3", len(result.Sections))
	}

	// Section 1: clauses and sub-clauses
	s1 := result.Sections[0]
	if s1.Number != "1" {
		t.Errorf("section 1 number = %q, want %q", s1.Number, "1")
	}
	if s1.Title != "Предмет договора" {
		t.Errorf("section 1 title = %q, want %q", s1.Title, "Предмет договора")
	}
	if len(s1.Clauses) != 2 {
		t.Fatalf("section 1: got %d clauses, want 2", len(s1.Clauses))
	}
	if s1.Clauses[0].Number != "1.1" {
		t.Errorf("clause number = %q, want %q", s1.Clauses[0].Number, "1.1")
	}
	if len(s1.Clauses[0].SubClauses) != 2 {
		t.Fatalf("clause 1.1: got %d sub-clauses, want 2", len(s1.Clauses[0].SubClauses))
	}
	if s1.Clauses[0].SubClauses[0].Number != "1.1.1" {
		t.Errorf("sub-clause number = %q, want %q", s1.Clauses[0].SubClauses[0].Number, "1.1.1")
	}
	if !strings.Contains(s1.Clauses[0].SubClauses[0].Content, "юридическое консультирование") {
		t.Errorf("sub-clause 1.1.1 content missing expected text, got %q", s1.Clauses[0].SubClauses[0].Content)
	}
	if s1.Clauses[1].Number != "1.2" {
		t.Errorf("clause number = %q, want %q", s1.Clauses[1].Number, "1.2")
	}

	// Section 2: clauses, no sub-clauses
	s2 := result.Sections[1]
	if s2.Number != "2" {
		t.Errorf("section 2 number = %q, want %q", s2.Number, "2")
	}
	if len(s2.Clauses) != 2 {
		t.Fatalf("section 2: got %d clauses, want 2", len(s2.Clauses))
	}

	// Section 3: content, no clauses
	s3 := result.Sections[2]
	if s3.Number != "3" {
		t.Errorf("section 3 number = %q, want %q", s3.Number, "3")
	}
	if s3.Content == "" {
		t.Error("section 3 should have content")
	}
	if len(s3.Clauses) != 0 {
		t.Errorf("section 3: got %d clauses, want 0", len(s3.Clauses))
	}

	// Appendices
	if len(result.Appendices) != 2 {
		t.Fatalf("got %d appendices, want 2", len(result.Appendices))
	}
	if result.Appendices[0].Number != "1" {
		t.Errorf("appendix 1 number = %q", result.Appendices[0].Number)
	}
	if !strings.Contains(result.Appendices[0].Title, "Перечень услуг") {
		t.Errorf("appendix 1 title = %q", result.Appendices[0].Title)
	}
	if !strings.Contains(result.Appendices[0].Content, "Детальное описание") {
		t.Errorf("appendix 1 content missing expected text")
	}

	// Party details
	if len(result.PartyDetails) != 2 {
		t.Fatalf("got %d party details, want 2", len(result.PartyDetails))
	}
	p1 := result.PartyDetails[0]
	if !strings.Contains(p1.Name, "Ромашка") {
		t.Errorf("party 1 name = %q, expected to contain Ромашка", p1.Name)
	}
	if p1.INN != "7701234567" {
		t.Errorf("party 1 INN = %q, want %q", p1.INN, "7701234567")
	}
	if p1.OGRN != "1027700132195" {
		t.Errorf("party 1 OGRN = %q, want %q", p1.OGRN, "1027700132195")
	}
	if !strings.Contains(p1.Address, "Москва") {
		t.Errorf("party 1 address = %q, expected to contain Москва", p1.Address)
	}
	if !strings.Contains(p1.Representative, "Иванов") {
		t.Errorf("party 1 representative = %q, expected to contain Иванов", p1.Representative)
	}

	p2 := result.PartyDetails[1]
	if !strings.Contains(p2.Name, "Петров") {
		t.Errorf("party 2 name = %q, expected to contain Петров", p2.Name)
	}
	if p2.INN != "770987654321" {
		t.Errorf("party 2 INN = %q, want %q", p2.INN, "770987654321")
	}

	// No critical warnings expected for a well-formed contract.
	if hasWarning(warnings, WarnEmptyText) {
		t.Error("unexpected STRUCTURE_EMPTY_TEXT warning")
	}
	if hasWarning(warnings, WarnNoSections) {
		t.Error("unexpected STRUCTURE_NO_SECTIONS warning")
	}
}

func TestExtract_SectionsOnly(t *testing.T) {
	ext := NewExtractor()
	ctx := context.Background()
	input := textFromString("doc-sections", sectionsOnlyText)

	result, warnings, err := ext.Extract(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Sections) != 3 {
		t.Fatalf("got %d sections, want 3", len(result.Sections))
	}

	// Each section should have content but no clauses.
	for i, s := range result.Sections {
		if s.Content == "" {
			t.Errorf("section %d (%s) has empty content", i+1, s.Title)
		}
		if len(s.Clauses) != 0 {
			t.Errorf("section %d (%s) has %d clauses, want 0", i+1, s.Title, len(s.Clauses))
		}
	}

	if hasWarning(warnings, WarnNoSections) {
		t.Error("unexpected STRUCTURE_NO_SECTIONS warning for sectioned text")
	}
}

func TestExtract_PartyDetailsRecognition(t *testing.T) {
	ext := NewExtractor()
	ctx := context.Background()
	input := textFromString("doc-party", partyDetailsText)

	result, _, err := ext.Extract(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.PartyDetails) != 2 {
		t.Fatalf("got %d parties, want 2", len(result.PartyDetails))
	}

	// Party 1: ЗАО "Альфа"
	p1 := result.PartyDetails[0]
	if !strings.Contains(p1.Name, "Альфа") {
		t.Errorf("party 1 name = %q, want to contain Альфа", p1.Name)
	}
	if p1.INN != "7707123456" {
		t.Errorf("party 1 INN = %q, want %q", p1.INN, "7707123456")
	}
	if p1.OGRN != "1037739169335" {
		t.Errorf("party 1 OGRN = %q, want %q", p1.OGRN, "1037739169335")
	}
	if !strings.Contains(p1.Address, "Санкт-Петербург") {
		t.Errorf("party 1 address = %q, want to contain Санкт-Петербург", p1.Address)
	}
	if !strings.Contains(p1.Representative, "Сидоров") {
		t.Errorf("party 1 representative = %q, want to contain Сидоров", p1.Representative)
	}

	// Party 2: ООО "Бета"
	p2 := result.PartyDetails[1]
	if !strings.Contains(p2.Name, "Бета") {
		t.Errorf("party 2 name = %q, want to contain Бета", p2.Name)
	}
	if p2.INN != "771234567890" {
		t.Errorf("party 2 INN = %q, want %q", p2.INN, "771234567890")
	}
	if p2.OGRN != "5177746100001" {
		t.Errorf("party 2 OGRN = %q, want %q", p2.OGRN, "5177746100001")
	}
	if !strings.Contains(p2.Address, "Казань") {
		t.Errorf("party 2 address = %q, want to contain Казань", p2.Address)
	}
	if !strings.Contains(p2.Representative, "Козлова") {
		t.Errorf("party 2 representative = %q, want to contain Козлова", p2.Representative)
	}
}

func TestExtract_EmptyText(t *testing.T) {
	ext := NewExtractor()
	ctx := context.Background()

	tests := []struct {
		name  string
		input *model.ExtractedText
	}{
		{
			name: "no pages",
			input: &model.ExtractedText{
				DocumentID: "doc-empty-1",
				Pages:      nil,
			},
		},
		{
			name: "single empty page",
			input: &model.ExtractedText{
				DocumentID: "doc-empty-2",
				Pages:      []model.PageText{{PageNumber: 1, Text: ""}},
			},
		},
		{
			name: "whitespace only",
			input: &model.ExtractedText{
				DocumentID: "doc-empty-3",
				Pages:      []model.PageText{{PageNumber: 1, Text: "   \n\t\n  "}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, warnings, err := ext.Extract(ctx, tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.DocumentID != tt.input.DocumentID {
				t.Errorf("DocumentID = %q, want %q", result.DocumentID, tt.input.DocumentID)
			}
			if len(result.Sections) != 0 {
				t.Errorf("got %d sections, want 0", len(result.Sections))
			}
			if !hasWarning(warnings, WarnEmptyText) {
				t.Error("expected STRUCTURE_EMPTY_TEXT warning")
			}
		})
	}
}

func TestExtract_NoStructure(t *testing.T) {
	ext := NewExtractor()
	ctx := context.Background()
	input := textFromString("doc-plain", noStructureText)

	result, warnings, err := ext.Extract(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.DocumentID != "doc-plain" {
		t.Errorf("DocumentID = %q, want %q", result.DocumentID, "doc-plain")
	}
	if len(result.Sections) != 0 {
		t.Errorf("got %d sections, want 0", len(result.Sections))
	}
	if !hasWarning(warnings, WarnNoSections) {
		t.Error("expected STRUCTURE_NO_SECTIONS warning")
	}
}

func TestExtract_MultipleAppendices(t *testing.T) {
	ext := NewExtractor()
	ctx := context.Background()
	input := textFromString("doc-app", multipleAppendicesText)

	result, _, err := ext.Extract(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Appendices) != 3 {
		t.Fatalf("got %d appendices, want 3", len(result.Appendices))
	}

	// Verify each appendix.
	expectations := []struct {
		number      string
		titlePart   string
		contentPart string
	}{
		{"1", "Спецификация", "Товар А"},
		{"2", "График", "Январь"},
		{"3", "Форма акта", "Акт сверки"},
	}
	for i, exp := range expectations {
		a := result.Appendices[i]
		if a.Number != exp.number {
			t.Errorf("appendix %d number = %q, want %q", i+1, a.Number, exp.number)
		}
		if !strings.Contains(a.Title, exp.titlePart) {
			t.Errorf("appendix %d title = %q, want to contain %q", i+1, a.Title, exp.titlePart)
		}
		if !strings.Contains(a.Content, exp.contentPart) {
			t.Errorf("appendix %d content = %q, want to contain %q", i+1, a.Content, exp.contentPart)
		}
	}
}

func TestExtract_MultipleParties(t *testing.T) {
	ext := NewExtractor()
	ctx := context.Background()

	text := `1. Предмет договора
1.1. Стороны договорились о нижеследующем.

Реквизиты сторон:

Арендодатель:
ООО "Первая компания"
ИНН: 7701111111
ОГРН: 1027700000001
Адрес: г. Москва, ул. Тверская, д. 1
Генеральный директор: Смирнов А.А.

Арендатор:
ЗАО "Вторая компания"
ИНН: 7702222222
ОГРН: 1027700000002
Адрес: г. Москва, ул. Арбат, д. 2
В лице: Кузнецов Б.Б.

Гарант:
АО "Третья компания"
ИНН: 7703333333
Адрес: г. Москва, ул. Мира, д. 3`

	input := textFromString("doc-multi-party", text)
	result, _, err := ext.Extract(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.PartyDetails) != 3 {
		t.Fatalf("got %d parties, want 3", len(result.PartyDetails))
	}

	if !strings.Contains(result.PartyDetails[0].Name, "Первая компания") {
		t.Errorf("party 1 name = %q", result.PartyDetails[0].Name)
	}
	if !strings.Contains(result.PartyDetails[1].Name, "Вторая компания") {
		t.Errorf("party 2 name = %q", result.PartyDetails[1].Name)
	}
	if !strings.Contains(result.PartyDetails[2].Name, "Третья компания") {
		t.Errorf("party 3 name = %q", result.PartyDetails[2].Name)
	}

	if result.PartyDetails[0].OGRN != "1027700000001" {
		t.Errorf("party 1 OGRN = %q", result.PartyDetails[0].OGRN)
	}
	if result.PartyDetails[1].Representative != "Кузнецов Б.Б." {
		t.Errorf("party 2 representative = %q", result.PartyDetails[1].Representative)
	}
}

func TestExtract_CompileTimeInterfaceCheck(t *testing.T) {
	// This is enforced at compile time by the package-level var in extractor.go,
	// but we explicitly verify the assignment works at runtime too.
	var iface port.StructureExtractionPort = NewExtractor()
	if iface == nil {
		t.Fatal("NewExtractor() should implement StructureExtractionPort")
	}
}

func TestExtract_ContextCancellation(t *testing.T) {
	ext := NewExtractor()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	input := textFromString("doc-cancelled", fullContractText)

	_, _, err := ext.Extract(ctx, input)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if !port.IsDomainError(err) {
		t.Errorf("expected DomainError, got %T", err)
	}
	if port.ErrorCode(err) != port.ErrCodeExtractionFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeExtractionFailed)
	}
}

func TestExtract_SectionWithContentNoClauses(t *testing.T) {
	ext := NewExtractor()
	ctx := context.Background()

	text := `1. Общие положения
Настоящий договор регулирует отношения между сторонами.
Действие договора распространяется на территорию РФ.

2. Срок действия
Договор вступает в силу с момента подписания.
Договор действует до 31.12.2026.`

	input := textFromString("doc-content", text)
	result, warnings, err := ext.Extract(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Sections) != 2 {
		t.Fatalf("got %d sections, want 2", len(result.Sections))
	}

	s1 := result.Sections[0]
	if s1.Number != "1" || s1.Title != "Общие положения" {
		t.Errorf("section 1: number=%q title=%q", s1.Number, s1.Title)
	}
	if !strings.Contains(s1.Content, "регулирует отношения") {
		t.Errorf("section 1 content = %q, want to contain 'регулирует отношения'", s1.Content)
	}
	if !strings.Contains(s1.Content, "территорию РФ") {
		t.Errorf("section 1 content = %q, want to contain 'территорию РФ'", s1.Content)
	}
	if len(s1.Clauses) != 0 {
		t.Errorf("section 1 has %d clauses, want 0", len(s1.Clauses))
	}

	s2 := result.Sections[1]
	if s2.Number != "2" || s2.Title != "Срок действия" {
		t.Errorf("section 2: number=%q title=%q", s2.Number, s2.Title)
	}
	if !strings.Contains(s2.Content, "момента подписания") {
		t.Errorf("section 2 content = %q, want to contain 'момента подписания'", s2.Content)
	}
	if len(s2.Clauses) != 0 {
		t.Errorf("section 2 has %d clauses, want 0", len(s2.Clauses))
	}

	if hasWarning(warnings, WarnNoSections) {
		t.Error("unexpected STRUCTURE_NO_SECTIONS warning")
	}
}

func TestExtract_WarningStage(t *testing.T) {
	ext := NewExtractor()
	ctx := context.Background()

	// Empty text produces a warning — verify its Stage field.
	input := &model.ExtractedText{DocumentID: "doc-stage", Pages: nil}
	_, warnings, err := ext.Extract(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(warnings) == 0 {
		t.Fatal("expected at least one warning")
	}
	for _, w := range warnings {
		if w.Stage != model.ProcessingStageStructureExtract {
			t.Errorf("warning %q has stage %q, want %q", w.Code, w.Stage, model.ProcessingStageStructureExtract)
		}
	}
}

func TestExtract_PartialPartyDetailsWarning(t *testing.T) {
	ext := NewExtractor()
	ctx := context.Background()

	// A party block with name but no INN and no address triggers partial warning.
	text := `1. Предмет договора
1.1. Тестовый пункт.

Реквизиты сторон:

Заказчик:
ООО "Полная информация"
ИНН: 7701234567
Адрес: г. Москва, ул. Тестовая, д. 1

Исполнитель:
Физлицо Иванов И.И.`

	input := textFromString("doc-partial", text)
	_, warnings, err := ext.Extract(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The second party has no INN and no Address — should be flagged as partial.
	if !hasWarning(warnings, WarnPartialPartyDetail) {
		t.Error("expected STRUCTURE_PARTIAL_PARTY_DETAILS warning for incomplete party")
	}
}

func TestExtract_SubClauseContent(t *testing.T) {
	ext := NewExtractor()
	ctx := context.Background()

	text := `1. Предмет договора
1.1. Исполнитель оказывает следующие услуги:
1.1.1. Юридическое сопровождение сделок.
1.1.2. Подготовка и проверка договоров.
1.1.3. Представительство в суде.`

	input := textFromString("doc-subclauses", text)
	result, _, err := ext.Extract(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Sections) != 1 {
		t.Fatalf("got %d sections, want 1", len(result.Sections))
	}
	if len(result.Sections[0].Clauses) != 1 {
		t.Fatalf("got %d clauses, want 1", len(result.Sections[0].Clauses))
	}
	clause := result.Sections[0].Clauses[0]
	if len(clause.SubClauses) != 3 {
		t.Fatalf("got %d sub-clauses, want 3", len(clause.SubClauses))
	}
	if clause.SubClauses[0].Number != "1.1.1" {
		t.Errorf("sub-clause 1 number = %q", clause.SubClauses[0].Number)
	}
	if !strings.Contains(clause.SubClauses[2].Content, "Представительство") {
		t.Errorf("sub-clause 3 content = %q", clause.SubClauses[2].Content)
	}
}

func TestExtract_MultiPageDocument(t *testing.T) {
	ext := NewExtractor()
	ctx := context.Background()

	input := &model.ExtractedText{
		DocumentID: "doc-multipage",
		Pages: []model.PageText{
			{PageNumber: 1, Text: "1. Предмет договора\n1.1. Исполнитель оказывает услуги."},
			{PageNumber: 2, Text: "2. Стоимость услуг\n2.1. Стоимость составляет 50 000 рублей."},
		},
	}

	result, _, err := ext.Extract(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Sections) != 2 {
		t.Fatalf("got %d sections, want 2", len(result.Sections))
	}
	if result.Sections[0].Title != "Предмет договора" {
		t.Errorf("section 1 title = %q", result.Sections[0].Title)
	}
	if result.Sections[1].Title != "Стоимость услуг" {
		t.Errorf("section 2 title = %q", result.Sections[1].Title)
	}
}

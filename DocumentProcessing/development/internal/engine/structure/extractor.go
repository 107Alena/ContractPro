package structure

import (
	"context"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// Warning codes emitted by the structure extraction engine.
const (
	WarnEmptyText          = "STRUCTURE_EMPTY_TEXT"
	WarnNoSections         = "STRUCTURE_NO_SECTIONS"
	WarnPartialPartyDetail = "STRUCTURE_PARTIAL_PARTY_DETAILS"
)

// Regular expressions for structure recognition.
var (
	// Section: starts with "1. Title" where title begins with an uppercase letter.
	reSection = regexp.MustCompile(`^(\d+)\.\s+(.+)`)

	// Clause: starts with "1.1." or "1.1)" or "1.1 ".
	reClause = regexp.MustCompile(`^(\d+\.\d+)[.\s)](.*)`)

	// Sub-clause: starts with "1.1.1." or "1.1.1)" or "1.1.1 ".
	reSubClause = regexp.MustCompile(`^(\d+\.\d+\.\d+)[.\s)](.*)`)

	// Appendix: starts with "Приложение N" or "Приложение №N".
	reAppendix = regexp.MustCompile(`(?i)^приложение\s+[№]?\s*(\d+)[.\s]*(.*)$`)

	// Party details marker: line containing "реквизиты сторон" or just "реквизиты".
	rePartyMarker = regexp.MustCompile(`(?i)реквизиты\s*сторон|(?i)реквизиты`)

	// INN: 10 or 12 digits.
	reINN = regexp.MustCompile(`(?i)^инн[:\s]*(\d{10,12})`)

	// OGRN: 13 or 15 digits.
	reOGRN = regexp.MustCompile(`(?i)^огрн[:\s]*(\d{13,15})`)

	// Address field.
	reAddress = regexp.MustCompile(`(?i)^(?:адрес|юридический\s+адрес)[:\s]*(.+)`)

	// Representative field.
	reRepresentative = regexp.MustCompile(`(?i)^(?:представитель|в\s+лице|генеральный\s+директор)[:\s]*(.+)`)
)

// Extractor implements StructureExtractionPort — extracts logical document
// structure (sections, clauses, sub-clauses, appendices, party details)
// from extracted text of a Russian legal contract (FR-1.3.1, FR-1.3.2).
type Extractor struct{}

// NewExtractor creates a new structure extraction engine.
func NewExtractor() *Extractor {
	return &Extractor{}
}

// Extract parses the extracted text into a DocumentStructure with sections,
// clauses, sub-clauses, appendices, and party details. Non-fatal issues
// are reported as warnings rather than errors.
func (e *Extractor) Extract(ctx context.Context, text *model.ExtractedText) (*model.DocumentStructure, []model.ProcessingWarning, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, port.NewExtractionError("context cancelled before structure extraction", err)
	}

	result := &model.DocumentStructure{
		DocumentID: text.DocumentID,
	}

	fullText := text.FullText()
	if strings.TrimSpace(fullText) == "" {
		return result, []model.ProcessingWarning{
			{
				Code:    WarnEmptyText,
				Message: "input text is empty, no structure can be extracted",
				Stage:   model.ProcessingStageStructureExtract,
			},
		}, nil
	}

	var warnings []model.ProcessingWarning

	lines := strings.Split(fullText, "\n")

	// Step 1: Extract party details block (typically near end of document).
	partyDetails, partyWarnings, partyStart, partyEnd := extractPartyDetails(lines)
	result.PartyDetails = partyDetails
	warnings = append(warnings, partyWarnings...)

	// Remove party details lines from further processing.
	remaining := excludeRange(lines, partyStart, partyEnd)

	// Step 2: Extract appendices.
	appendices, bodyLines := extractAppendices(remaining)
	result.Appendices = appendices

	// Step 3: Extract sections, clauses, sub-clauses from main body.
	sections := extractSections(bodyLines)
	result.Sections = sections

	if len(sections) == 0 {
		warnings = append(warnings, model.ProcessingWarning{
			Code:    WarnNoSections,
			Message: "no numbered sections found in document",
			Stage:   model.ProcessingStageStructureExtract,
		})
	}

	return result, warnings, nil
}

// extractPartyDetails scans lines for a party details block and parses
// individual parties. Returns the parsed details, any warnings, and the
// start/end line indices of the block (-1/-1 if not found).
func extractPartyDetails(lines []string) ([]model.PartyDetails, []model.ProcessingWarning, int, int) {
	markerIdx := -1
	// Search from the end — party details are typically at the bottom.
	for i := len(lines) - 1; i >= 0; i-- {
		if rePartyMarker.MatchString(strings.TrimSpace(lines[i])) {
			markerIdx = i
			break
		}
	}
	if markerIdx < 0 {
		return nil, nil, -1, -1
	}

	// Everything from marker to end is the party details block.
	partyLines := lines[markerIdx+1:]
	parties, warnings := parseParties(partyLines)

	return parties, warnings, markerIdx, len(lines)
}

// parseParties splits the party details block into individual parties
// separated by empty lines, then extracts fields from each.
func parseParties(lines []string) ([]model.PartyDetails, []model.ProcessingWarning) {
	var parties []model.PartyDetails
	var warnings []model.ProcessingWarning

	// Split into blocks separated by empty lines.
	var blocks [][]string
	var current []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(current) > 0 {
				blocks = append(blocks, current)
				current = nil
			}
			continue
		}
		current = append(current, trimmed)
	}
	if len(current) > 0 {
		blocks = append(blocks, current)
	}

	for _, block := range blocks {
		party, partial := parsePartyBlock(block)
		if party.Name == "" {
			continue
		}
		parties = append(parties, party)
		if partial {
			warnings = append(warnings, model.ProcessingWarning{
				Code:    WarnPartialPartyDetail,
				Message: "party details block found but some fields could not be parsed for party: " + party.Name,
				Stage:   model.ProcessingStageStructureExtract,
			})
		}
	}

	return parties, warnings
}

// parsePartyBlock extracts party details from a block of lines.
// The first line is treated as the party name/role header.
// Returns the party and whether parsing was partial (missing key fields).
func parsePartyBlock(block []string) (model.PartyDetails, bool) {
	if len(block) == 0 {
		return model.PartyDetails{}, false
	}

	party := model.PartyDetails{}

	// First line may be a role label like "Заказчик:" — skip it and
	// use the second line as name, or treat the first line as name
	// if no second line with a company-like name exists.
	startIdx := 0
	if len(block) > 1 && isRoleLabel(block[0]) {
		// Role label line — the actual company name follows.
		startIdx = 1
	}

	if startIdx < len(block) {
		party.Name = block[startIdx]
		startIdx++
	}

	// Parse labeled fields from remaining lines.
	for i := startIdx; i < len(block); i++ {
		line := block[i]
		switch {
		case reINN.MatchString(line):
			m := reINN.FindStringSubmatch(line)
			party.INN = m[1]
		case reOGRN.MatchString(line):
			m := reOGRN.FindStringSubmatch(line)
			party.OGRN = m[1]
		case reAddress.MatchString(line):
			m := reAddress.FindStringSubmatch(line)
			party.Address = strings.TrimSpace(m[1])
		case reRepresentative.MatchString(line):
			m := reRepresentative.FindStringSubmatch(line)
			party.Representative = strings.TrimSpace(m[1])
		}
	}

	// Consider partial if we have a name but no INN and no address.
	partial := party.INN == "" && party.Address == ""

	return party, partial
}

// isRoleLabel checks if a line looks like a party role label
// (e.g. "Заказчик:", "Исполнитель:", "Продавец:").
func isRoleLabel(line string) bool {
	trimmed := strings.TrimRight(strings.TrimSpace(line), ":")
	// A role label is typically a single word or short phrase,
	// and does not contain digits or quotes (which would indicate a company name).
	if strings.ContainsAny(trimmed, "\"'«»0123456789") {
		return false
	}
	// Must be a short phrase (1-3 words).
	words := strings.Fields(trimmed)
	return len(words) >= 1 && len(words) <= 3
}

// excludeRange returns lines with the range [start, end) removed.
// If start < 0, returns lines unchanged.
func excludeRange(lines []string, start, end int) []string {
	if start < 0 {
		return lines
	}
	result := make([]string, 0, len(lines)-(end-start))
	result = append(result, lines[:start]...)
	if end < len(lines) {
		result = append(result, lines[end:]...)
	}
	return result
}

// extractAppendices finds appendix markers and extracts appendix blocks.
// Returns the parsed appendices and the remaining body lines (without appendices).
func extractAppendices(lines []string) ([]model.Appendix, []string) {
	type appendixPos struct {
		idx    int
		number string
		title  string
	}

	var positions []appendixPos
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		m := reAppendix.FindStringSubmatch(trimmed)
		if m != nil {
			positions = append(positions, appendixPos{
				idx:    i,
				number: m[1],
				title:  strings.TrimSpace(m[2]),
			})
		}
	}

	if len(positions) == 0 {
		return nil, lines
	}

	var appendices []model.Appendix
	for i, pos := range positions {
		var contentLines []string
		var endIdx int
		if i+1 < len(positions) {
			endIdx = positions[i+1].idx
		} else {
			endIdx = len(lines)
		}
		for j := pos.idx + 1; j < endIdx; j++ {
			contentLines = append(contentLines, lines[j])
		}
		appendices = append(appendices, model.Appendix{
			Number:  pos.number,
			Title:   pos.title,
			Content: strings.TrimSpace(strings.Join(contentLines, "\n")),
		})
	}

	// Body is everything before the first appendix.
	bodyLines := lines[:positions[0].idx]
	return appendices, bodyLines
}

// extractSections parses sections, clauses, and sub-clauses from body lines.
func extractSections(lines []string) []model.Section {
	var sections []model.Section

	var curSection *model.Section
	var curClause *model.Clause

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Try sub-clause first (most specific pattern).
		if m := reSubClause.FindStringSubmatch(trimmed); m != nil {
			sc := model.SubClause{
				Number:  m[1],
				Content: strings.TrimSpace(m[2]),
			}
			if curClause != nil {
				curClause.SubClauses = append(curClause.SubClauses, sc)
			} else if curSection != nil {
				// Sub-clause without parent clause — create an implicit clause.
				curSection.Clauses = append(curSection.Clauses, model.Clause{
					Number:     m[1],
					Content:    strings.TrimSpace(m[2]),
					SubClauses: nil,
				})
			}
			continue
		}

		// Try clause.
		if m := reClause.FindStringSubmatch(trimmed); m != nil {
			// Flush current clause into current section.
			flushClause(&sections, curSection, curClause)
			curClause = &model.Clause{
				Number:  m[1],
				Content: strings.TrimSpace(m[2]),
			}
			if curSection == nil {
				// Clause without a parent section — create an implicit section.
				curSection = &model.Section{
					Number: strings.Split(m[1], ".")[0],
					Title:  "",
				}
			}
			continue
		}

		// Try section.
		if m := reSection.FindStringSubmatch(trimmed); m != nil {
			title := strings.TrimSpace(m[2])
			if startsWithUppercaseRussian(title) || isKnownHeader(title) {
				// Flush current clause and section.
				flushClause(&sections, curSection, curClause)
				curClause = nil
				flushSection(&sections, curSection)
				curSection = &model.Section{
					Number: m[1],
					Title:  title,
				}
				continue
			}
		}

		// Content line — append to current clause or section.
		if curClause != nil {
			if curClause.Content == "" {
				curClause.Content = trimmed
			} else {
				curClause.Content += " " + trimmed
			}
		} else if curSection != nil {
			if curSection.Content == "" {
				curSection.Content = trimmed
			} else {
				curSection.Content += " " + trimmed
			}
		}
		// Lines before any section/clause are ignored (e.g. document title).
	}

	// Flush remaining.
	flushClause(&sections, curSection, curClause)
	flushSection(&sections, curSection)

	return sections
}

// flushClause appends the current clause to the current section, if both are non-nil.
func flushClause(sections *[]model.Section, curSection *model.Section, curClause *model.Clause) {
	if curClause == nil || curSection == nil {
		return
	}
	curSection.Clauses = append(curSection.Clauses, *curClause)
}

// flushSection appends the current section to the sections slice, if non-nil.
func flushSection(sections *[]model.Section, curSection *model.Section) {
	if curSection == nil {
		return
	}
	*sections = append(*sections, *curSection)
}

// startsWithUppercaseRussian checks whether the string starts with an uppercase
// Cyrillic (Russian) letter.
func startsWithUppercaseRussian(s string) bool {
	if s == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.Is(unicode.Cyrillic, r) && unicode.IsUpper(r)
}

// isKnownHeader checks whether the title matches a known Russian contract section header.
func isKnownHeader(title string) bool {
	lower := strings.ToLower(strings.TrimSpace(title))
	known := []string{
		"предмет договора",
		"предмет",
		"стоимость",
		"цена",
		"стоимость услуг",
		"порядок расчётов",
		"порядок расчетов",
		"сроки",
		"сроки оказания услуг",
		"права и обязанности",
		"ответственность сторон",
		"ответственность",
		"конфиденциальность",
		"форс-мажор",
		"разрешение споров",
		"заключительные положения",
		"прочие условия",
		"срок действия",
		"порядок оказания услуг",
		"гарантии",
		"общие положения",
		"определения",
		"термины и определения",
	}
	for _, h := range known {
		if strings.HasPrefix(lower, h) {
			return true
		}
	}
	return false
}

// compile-time check: Extractor implements StructureExtractionPort.
var _ port.StructureExtractionPort = (*Extractor)(nil)

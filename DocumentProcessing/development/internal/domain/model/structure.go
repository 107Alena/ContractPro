package model

// SubClause represents a sub-clause within a clause (e.g. 1.1.1).
type SubClause struct {
	Number  string `json:"number"`
	Content string `json:"content"`
}

// Clause represents a clause within a section (e.g. 1.1).
type Clause struct {
	Number     string      `json:"number"`
	Content    string      `json:"content"`
	SubClauses []SubClause `json:"sub_clauses,omitempty"`
}

// Section represents a top-level section of the document.
type Section struct {
	Number  string   `json:"number"`
	Title   string   `json:"title"`
	Content string   `json:"content,omitempty"`
	Clauses []Clause `json:"clauses,omitempty"`
}

// Appendix represents an appendix of the document.
type Appendix struct {
	Number  string `json:"number"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

// PartyDetails holds the legal details (requisites) of a contract party.
type PartyDetails struct {
	Name           string `json:"name"`
	INN            string `json:"inn,omitempty"`
	OGRN           string `json:"ogrn,omitempty"`
	Address        string `json:"address,omitempty"`
	Representative string `json:"representative,omitempty"`
}

// DocumentStructure represents the logical structure of a document.
type DocumentStructure struct {
	DocumentID   string         `json:"document_id"`
	Sections     []Section      `json:"sections"`
	Appendices   []Appendix     `json:"appendices,omitempty"`
	PartyDetails []PartyDetails `json:"party_details,omitempty"`
}

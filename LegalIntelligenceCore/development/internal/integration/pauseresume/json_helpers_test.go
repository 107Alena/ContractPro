package pauseresume_test

import "encoding/json"

// jsonMarshal wraps encoding/json.Marshal so the pause+resume scenario
// file does not have to import encoding/json directly (keeps the
// scenario file focused on assertions). Mirrors the happypath helper.
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// jsonUnmarshal mirrors jsonMarshal for the poll helpers.
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

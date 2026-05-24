package happypath_test

import "encoding/json"

// jsonMarshal wraps encoding/json.Marshal so the happy-path test does
// not have to import encoding/json directly in the file that drives the
// pipeline scenario (keeps the scenario file focused on assertions).
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// jsonUnmarshal mirrors jsonMarshal for the poll helper.
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

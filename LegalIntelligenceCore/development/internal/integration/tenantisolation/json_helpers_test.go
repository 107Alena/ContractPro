package tenantisolation_test

import "encoding/json"

// jsonMarshal wraps encoding/json.Marshal so the tenant-isolation
// scenario file does not have to import encoding/json directly (keeps
// the scenario file focused on assertions). Mirrors the happypath and
// pauseresume helpers.
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// jsonUnmarshal mirrors jsonMarshal for the poll helpers and the
// status-changed loop in the resume test.
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

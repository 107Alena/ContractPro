package permissions

import (
	"encoding/json"
)

// opmPolicy mirrors the minimal shape the resolver needs from each OPM policy.
// Additional fields in the OPM response are ignored (ASSUMPTION-ORCH-11:
// OPM returns a JSON array of objects with at least {name: string, enabled: bool}).
type opmPolicy struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// policyBusinessUserExport is the OPM policy name controlling export_enabled
// for BUSINESS_USER. See high-architecture.md §6.21.
const policyBusinessUserExport = "business_user_export"

// parseBusinessUserExportFlag extracts the `enabled` flag of the
// business_user_export policy from an OPM ListPolicies response.
//
// Return values:
//   - enabled: the flag value (valid only when found == true).
//   - found:   true if policy exists in the response.
//   - err:     non-nil if the JSON payload is not a policy array.
//
// A missing policy is NOT an error — it signals "use environment fallback".
func parseBusinessUserExportFlag(raw json.RawMessage) (enabled bool, found bool, err error) {
	if len(raw) == 0 {
		return false, false, nil
	}

	var policies []opmPolicy
	if uerr := json.Unmarshal(raw, &policies); uerr != nil {
		return false, false, uerr
	}

	for _, p := range policies {
		if p.Name == policyBusinessUserExport {
			return p.Enabled, true, nil
		}
	}
	return false, false, nil
}

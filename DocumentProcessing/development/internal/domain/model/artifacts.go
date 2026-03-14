package model

// TemporaryArtifacts tracks temporary files stored in object storage during job processing.
type TemporaryArtifacts struct {
	JobID       string   `json:"job_id"`
	StorageKeys []string `json:"storage_keys"`
}

// AddKey appends a storage key to the artifact list.
func (a *TemporaryArtifacts) AddKey(key string) {
	a.StorageKeys = append(a.StorageKeys, key)
}

// HasKeys returns true if there are any tracked storage keys.
func (a *TemporaryArtifacts) HasKeys() bool {
	return len(a.StorageKeys) > 0
}

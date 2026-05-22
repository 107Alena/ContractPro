package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"contractpro/legal-intelligence-core/internal/application/pipeline"
	ingressrouter "contractpro/legal-intelligence-core/internal/ingress/router"
	"contractpro/legal-intelligence-core/internal/infra/kvstore"
)

// keyPrefixVersionMeta is the Redis key namespace for the version-meta cache
// (high-architecture.md §6.5/§8.3: parent_version_id + origin_type — populated
// on VersionCreated, read by the orchestrator's resolveParentAndMode).
const keyPrefixVersionMeta = "lic-version-meta:"

// versionMetaPayload is the on-wire shape stored in Redis for one version's
// metadata. Both ingress (router) and pipeline orchestrator agree on this
// shape because they are wired through this adapter.
type versionMetaPayload struct {
	ParentVersionID *string `json:"parent_version_id,omitempty"`
	OriginType      string  `json:"origin_type,omitempty"`
}

// versionMetaCache is the kvstore-backed bridge satisfying BOTH
// ingressrouter.VersionMetaCacheWriter (Set) and pipeline.VersionMetaCache
// (GetParentVersionID). The same Redis namespace is therefore the SSOT.
type versionMetaCache struct {
	kv *kvstore.Client
}

// newVersionMetaCache wires a *kvstore.Client into the two seams.
func newVersionMetaCache(kv *kvstore.Client) *versionMetaCache {
	return &versionMetaCache{kv: kv}
}

// Set stores opaque bytes in Redis with the given TTL. The Router marshals
// the payload itself; this adapter does not parse it.
func (c *versionMetaCache) Set(ctx context.Context, versionID string, payload []byte, ttl time.Duration) error {
	if versionID == "" {
		return errors.New("app/version-meta: versionID must not be empty")
	}
	return c.kv.Set(ctx, keyPrefixVersionMeta+versionID, string(payload), ttl)
}

// GetParentVersionID reads the cached payload and unmarshals
// parent_version_id. A miss returns (nil, nil) per the §8.3 degrade-to-INITIAL
// contract; a malformed payload is also treated as a miss but produces an
// error to aid debugging (the orchestrator treats it identically to a miss).
func (c *versionMetaCache) GetParentVersionID(ctx context.Context, versionID string) (*string, error) {
	if versionID == "" {
		return nil, nil
	}
	raw, err := c.kv.Get(ctx, keyPrefixVersionMeta+versionID)
	if err != nil {
		if errors.Is(err, kvstore.ErrKeyNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("app/version-meta: get: %w", err)
	}
	var p versionMetaPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("app/version-meta: unmarshal: %w", err)
	}
	return p.ParentVersionID, nil
}

var (
	_ ingressrouter.VersionMetaCacheWriter = (*versionMetaCache)(nil)
	_ pipeline.VersionMetaCache            = (*versionMetaCache)(nil)
)

// Package integration provides in-memory fakes and test infrastructure for
// end-to-end integration testing of the API/Backend Orchestrator.
//
// The fakes implement the same interfaces consumed by the production
// components (broker, KV store, object storage, DM REST API) and allow
// tests to exercise the full request path — HTTP handler -> application
// logic -> egress adapters — without any external dependencies.
package integration

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/infra/kvstore"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
	"contractpro/api-orchestrator/internal/ingress/sse"
)

// ---------------------------------------------------------------------------
// 1. fakeBroker — implements consumer.BrokerSubscriber and
//    commandpub.BrokerPublisher
// ---------------------------------------------------------------------------

// publishedMessage records a message published to the fake broker.
type publishedMessage struct {
	Topic   string
	Payload []byte
}

// fakeBroker is a thread-safe in-memory message broker that records published
// messages and allows test code to inject events into registered handlers.
type fakeBroker struct {
	mu        sync.Mutex
	handlers  map[string]func(ctx context.Context, body []byte) error
	published []publishedMessage
}

func newFakeBroker() *fakeBroker {
	return &fakeBroker{
		handlers:  make(map[string]func(ctx context.Context, body []byte) error),
		published: nil,
	}
}

// Subscribe stores the handler for the given topic.
// Satisfies consumer.BrokerSubscriber.
func (b *fakeBroker) Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[topic] = handler
	return nil
}

// Publish records the message for later inspection.
// Satisfies commandpub.BrokerPublisher.
func (b *fakeBroker) Publish(_ context.Context, topic string, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]byte, len(payload))
	copy(cp, payload)
	b.published = append(b.published, publishedMessage{Topic: topic, Payload: cp})
	return nil
}

// InjectEvent calls the registered handler for the given topic synchronously.
// Returns an error if no handler is registered.
func (b *fakeBroker) InjectEvent(topic string, payload []byte) error {
	b.mu.Lock()
	handler, ok := b.handlers[topic]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("fakeBroker: no handler registered for topic %q", topic)
	}
	return handler(context.Background(), payload)
}

// PublishedMessages returns a copy of all published messages.
func (b *fakeBroker) PublishedMessages() []publishedMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]publishedMessage, len(b.published))
	copy(cp, b.published)
	return cp
}

// ---------------------------------------------------------------------------
// 2. fakeObjectStorage — implements upload.ObjectStorage and
//    versions.ObjectStorage
// ---------------------------------------------------------------------------

// fakeObjectStorage is a thread-safe in-memory object store.
type fakeObjectStorage struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newFakeObjectStorage() *fakeObjectStorage {
	return &fakeObjectStorage{
		objects: make(map[string][]byte),
	}
}

// PutObject stores the data under the given key. The data is fully read from
// the ReadSeeker; contentType is ignored in tests.
func (s *fakeObjectStorage) PutObject(_ context.Context, key string, data io.ReadSeeker, _ string) error {
	raw, err := io.ReadAll(data)
	if err != nil {
		return fmt.Errorf("fakeObjectStorage: PutObject: read: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = raw
	return nil
}

// DeleteObject removes the object. No error if the key does not exist.
func (s *fakeObjectStorage) DeleteObject(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

// HasObject returns true if the key exists. Test helper.
func (s *fakeObjectStorage) HasObject(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.objects[key]
	return ok
}

// ---------------------------------------------------------------------------
// 3. fakeKVStore — satisfies statustracker.KVStore, ssebroadcast.Publisher,
//    sse.KVStore, and upload.KVStore
// ---------------------------------------------------------------------------

// fakeKVStore is a thread-safe in-memory key-value store with Pub/Sub support.
type fakeKVStore struct {
	mu          sync.Mutex
	store       map[string]string
	subscribers map[string][]chan string
}

func newFakeKVStore() *fakeKVStore {
	return &fakeKVStore{
		store:       make(map[string]string),
		subscribers: make(map[string][]chan string),
	}
}

// Get returns the value for the key. Returns kvstore.ErrKeyNotFound if missing.
// Satisfies statustracker.KVStore.
func (kv *fakeKVStore) Get(_ context.Context, key string) (string, error) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	v, ok := kv.store[key]
	if !ok {
		return "", kvstore.ErrKeyNotFound
	}
	return v, nil
}

// Set stores the value. TTL is ignored in tests.
// Satisfies statustracker.KVStore, sse.KVStore, upload.KVStore.
func (kv *fakeKVStore) Set(_ context.Context, key string, value string, _ time.Duration) error {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	kv.store[key] = value
	return nil
}

// Delete removes the key. No error if the key does not exist.
// Satisfies sse.KVStore.
func (kv *fakeKVStore) Delete(_ context.Context, key string) error {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	delete(kv.store, key)
	return nil
}

// Publish sends a message to all subscribers of the given channel.
// Satisfies ssebroadcast.Publisher.
func (kv *fakeKVStore) Publish(_ context.Context, channel string, message string) error {
	kv.mu.Lock()
	subs := make([]chan string, len(kv.subscribers[channel]))
	copy(subs, kv.subscribers[channel])
	kv.mu.Unlock()

	for _, ch := range subs {
		// Non-blocking send to avoid deadlock if subscriber is slow.
		select {
		case ch <- message:
		default:
		}
	}
	return nil
}

// Subscribe creates a subscription that delivers messages to handler in a
// background goroutine. Returns a Subscription that satisfies sse.Subscription.
// Satisfies sse.KVStore.
func (kv *fakeKVStore) Subscribe(ctx context.Context, channel string, handler func(msg string)) (sse.Subscription, error) {
	ch := make(chan string, 64)

	kv.mu.Lock()
	kv.subscribers[channel] = append(kv.subscribers[channel], ch)
	kv.mu.Unlock()

	subCtx, cancel := context.WithCancel(ctx)

	sub := &fakeSubscription{
		cancel:  cancel,
		channel: channel,
		ch:      ch,
		kv:      kv,
	}

	go func() {
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				handler(msg)
			case <-subCtx.Done():
				return
			}
		}
	}()

	return sub, nil
}

// SetDirect seeds a key-value pair without context. Test helper.
func (kv *fakeKVStore) SetDirect(key, value string) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	kv.store[key] = value
}

// fakeSubscription implements sse.Subscription.
type fakeSubscription struct {
	once    sync.Once
	cancel  func()
	channel string
	ch      chan string
	kv      *fakeKVStore
}

// Close stops the subscription goroutine and removes the subscriber channel.
func (s *fakeSubscription) Close() error {
	s.once.Do(func() {
		s.cancel()

		s.kv.mu.Lock()
		subs := s.kv.subscribers[s.channel]
		for i, sub := range subs {
			if sub == s.ch {
				s.kv.subscribers[s.channel] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		s.kv.mu.Unlock()
	})
	return nil
}

// ---------------------------------------------------------------------------
// 4. fakeDMServer — httptest.Server that mimics the DM REST API
// ---------------------------------------------------------------------------

// fakeDMServer is an in-memory DM REST API server backed by maps.
type fakeDMServer struct {
	server     *httptest.Server
	mu         sync.Mutex
	documents  map[string]*dmclient.Document
	versions   map[string]map[string]*dmclient.DocumentVersionWithArtifacts // docID -> verID -> version
	artifacts  map[string]json.RawMessage                                   // "docID:verID:type" -> content
	diffs      map[string]*dmclient.VersionDiff                             // "docID:baseVer:targetVer" -> diff
	nextVerNum int
}

func newFakeDMServer() *fakeDMServer {
	dm := &fakeDMServer{
		documents:  make(map[string]*dmclient.Document),
		versions:   make(map[string]map[string]*dmclient.DocumentVersionWithArtifacts),
		artifacts:  make(map[string]json.RawMessage),
		diffs:      make(map[string]*dmclient.VersionDiff),
		nextVerNum: 1,
	}

	r := chi.NewRouter()

	r.Post("/documents", dm.handleCreateDocument)
	r.Get("/documents/{id}", dm.handleGetDocument)
	r.Delete("/documents/{id}", dm.handleDeleteDocument)
	r.Post("/documents/{id}/archive", dm.handleArchiveDocument)

	r.Post("/documents/{id}/versions", dm.handleCreateVersion)
	r.Get("/documents/{id}/versions", dm.handleListVersions)
	r.Get("/documents/{id}/versions/{vid}", dm.handleGetVersion)
	r.Get("/documents/{id}/versions/{vid}/artifacts/{type}", dm.handleGetArtifact)

	r.Get("/documents/{id}/diffs/{base_vid}/{target_vid}", dm.handleGetDiff)

	dm.server = httptest.NewServer(r)
	return dm
}

func (dm *fakeDMServer) URL() string {
	return dm.server.URL
}

func (dm *fakeDMServer) Close() {
	dm.server.Close()
}

// --- Seed methods ---

func (dm *fakeDMServer) SeedDocument(doc *dmclient.Document) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.documents[doc.DocumentID] = doc
}

func (dm *fakeDMServer) SeedVersion(docID string, ver *dmclient.DocumentVersionWithArtifacts) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	if dm.versions[docID] == nil {
		dm.versions[docID] = make(map[string]*dmclient.DocumentVersionWithArtifacts)
	}
	dm.versions[docID][ver.VersionID] = ver
}

func (dm *fakeDMServer) SeedArtifact(docID, verID, artifactType string, content json.RawMessage) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	key := docID + ":" + verID + ":" + artifactType
	dm.artifacts[key] = content
}

func (dm *fakeDMServer) SeedDiff(docID, baseVer, targetVer string, diff *dmclient.VersionDiff) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	key := docID + ":" + baseVer + ":" + targetVer
	dm.diffs[key] = diff
}

// --- HTTP handlers ---

func (dm *fakeDMServer) handleCreateDocument(w http.ResponseWriter, r *http.Request) {
	var req dmclient.CreateDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	orgID := r.Header.Get("X-Organization-ID")
	userID := r.Header.Get("X-User-ID")

	doc := &dmclient.Document{
		DocumentID:      uuid.New().String(),
		OrganizationID:  orgID,
		Title:           req.Title,
		Status:          "ACTIVE",
		CreatedByUserID: userID,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}

	dm.mu.Lock()
	dm.documents[doc.DocumentID] = doc
	dm.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(doc)
}

func (dm *fakeDMServer) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	dm.mu.Lock()
	doc, ok := dm.documents[id]
	if !ok {
		dm.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error_code":"NOT_FOUND","message":"document not found"}`))
		return
	}

	// Build DocumentWithCurrentVersion.
	resp := dmclient.DocumentWithCurrentVersion{Document: *doc}
	if versions, hasVers := dm.versions[id]; hasVers {
		// Find the version with the highest version_number.
		var latest *dmclient.DocumentVersion
		for _, v := range versions {
			if latest == nil || v.VersionNumber > latest.VersionNumber {
				ver := v.DocumentVersion // copy
				latest = &ver
			}
		}
		if latest != nil {
			resp.CurrentVersion = latest
			cvID := latest.VersionID
			doc.CurrentVersionID = &cvID
		}
	}
	dm.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (dm *fakeDMServer) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	dm.mu.Lock()
	doc, ok := dm.documents[id]
	if !ok {
		dm.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error_code":"NOT_FOUND","message":"document not found"}`))
		return
	}
	doc.Status = "DELETED"
	doc.UpdatedAt = time.Now().UTC()
	dm.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(doc)
}

func (dm *fakeDMServer) handleArchiveDocument(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	dm.mu.Lock()
	doc, ok := dm.documents[id]
	if !ok {
		dm.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error_code":"NOT_FOUND","message":"document not found"}`))
		return
	}
	doc.Status = "ARCHIVED"
	doc.UpdatedAt = time.Now().UTC()
	dm.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(doc)
}

func (dm *fakeDMServer) handleCreateVersion(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "id")

	dm.mu.Lock()
	if _, ok := dm.documents[docID]; !ok {
		dm.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error_code":"NOT_FOUND","message":"document not found"}`))
		return
	}
	dm.mu.Unlock()

	var req dmclient.CreateVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	userID := r.Header.Get("X-User-ID")

	dm.mu.Lock()
	verNum := dm.nextVerNum
	dm.nextVerNum++

	ver := &dmclient.DocumentVersionWithArtifacts{
		DocumentVersion: dmclient.DocumentVersion{
			VersionID:          uuid.New().String(),
			DocumentID:         docID,
			VersionNumber:      verNum,
			OriginType:         req.OriginType,
			SourceFileKey:      req.SourceFileKey,
			SourceFileName:     req.SourceFileName,
			SourceFileSize:     req.SourceFileSize,
			SourceFileChecksum: req.SourceFileChecksum,
			ArtifactStatus:     "PENDING_PROCESSING",
			CreatedByUserID:    userID,
			CreatedAt:          time.Now().UTC(),
		},
		Artifacts: []dmclient.ArtifactDescriptor{},
	}
	if req.ParentVersionID != "" {
		pvid := req.ParentVersionID
		ver.ParentVersionID = &pvid
	}

	if dm.versions[docID] == nil {
		dm.versions[docID] = make(map[string]*dmclient.DocumentVersionWithArtifacts)
	}
	dm.versions[docID][ver.VersionID] = ver
	dm.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(ver.DocumentVersion)
}

func (dm *fakeDMServer) handleListVersions(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "id")

	dm.mu.Lock()
	if _, ok := dm.documents[docID]; !ok {
		dm.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error_code":"NOT_FOUND","message":"document not found"}`))
		return
	}

	var items []dmclient.DocumentVersion
	for _, v := range dm.versions[docID] {
		items = append(items, v.DocumentVersion)
	}
	dm.mu.Unlock()

	if items == nil {
		items = []dmclient.DocumentVersion{}
	}

	resp := dmclient.VersionList{
		Items: items,
		Total: len(items),
		Page:  1,
		Size:  len(items),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (dm *fakeDMServer) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "id")
	verID := chi.URLParam(r, "vid")

	dm.mu.Lock()
	verMap, ok := dm.versions[docID]
	if !ok {
		dm.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error_code":"NOT_FOUND","message":"version not found"}`))
		return
	}
	ver, ok := verMap[verID]
	if !ok {
		dm.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error_code":"NOT_FOUND","message":"version not found"}`))
		return
	}
	dm.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ver)
}

func (dm *fakeDMServer) handleGetArtifact(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "id")
	verID := chi.URLParam(r, "vid")
	artType := chi.URLParam(r, "type")

	key := docID + ":" + verID + ":" + artType

	dm.mu.Lock()
	content, ok := dm.artifacts[key]
	dm.mu.Unlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error_code":"NOT_FOUND","message":"artifact not found"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (dm *fakeDMServer) handleGetDiff(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "id")
	baseVID := chi.URLParam(r, "base_vid")
	targetVID := chi.URLParam(r, "target_vid")

	key := docID + ":" + baseVID + ":" + targetVID

	dm.mu.Lock()
	diff, ok := dm.diffs[key]
	dm.mu.Unlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error_code":"NOT_FOUND","message":"diff not found"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(diff)
}

// ---------------------------------------------------------------------------
// 5. testJWTSigner — generates valid JWTs for testing
// ---------------------------------------------------------------------------

// testJWTSigner generates valid ES256 JWTs for integration tests.
type testJWTSigner struct {
	privateKey *ecdsa.PrivateKey
	PublicKey  *ecdsa.PublicKey
}

func newTestJWTSigner() *testJWTSigner {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic("testJWTSigner: failed to generate ECDSA key: " + err.Error())
	}
	return &testJWTSigner{
		privateKey: key,
		PublicKey:  &key.PublicKey,
	}
}

// SignToken creates a signed JWT with the given claims.
func (s *testJWTSigner) SignToken(userID, orgID string, role auth.Role) string {
	now := time.Now()
	claims := auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ID:        uuid.New().String(),
			ExpiresAt: jwt.NewNumericDate(now.Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		Org:  orgID,
		Role: string(role),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	signed, err := token.SignedString(s.privateKey)
	if err != nil {
		panic("testJWTSigner: failed to sign token: " + err.Error())
	}
	return signed
}

// ---------------------------------------------------------------------------
// Compile-time interface checks
// ---------------------------------------------------------------------------

// Verify fakeKVStore satisfies the consumer-side interfaces. The checks use
// locally-scoped anonymous variables rather than package-level vars to avoid
// polluting the package namespace.
var (
	_ sse.KVStore  = (*fakeKVStore)(nil)
	_ sse.Subscription = (*fakeSubscription)(nil)
)

// Verify fakeBroker satisfies both broker interfaces. We cannot reference
// the concrete interface types from consumer/commandpub packages here
// because the Subscribe/Publish signatures match structurally.

// Helpers for DM diff key building used by test code.
func diffKey(docID, baseVer, targetVer string) string {
	return docID + ":" + baseVer + ":" + targetVer
}

// artifactKey builds the key for an artifact in the fake DM server.
func artifactKey(docID, verID, artifactType string) string {
	return docID + ":" + verID + ":" + artifactType
}

// Ensure fakeObjectStorage satisfies S3-related interfaces.
// The structural check is implicit via PutObject / DeleteObject signatures.

// --- helpers for reading DM test response bodies ---

// parseDMVersion is a convenience for tests that need to check fake DM
// version creation state.
func (dm *fakeDMServer) GetStoredVersion(docID, verID string) *dmclient.DocumentVersionWithArtifacts {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	if m, ok := dm.versions[docID]; ok {
		return m[verID]
	}
	return nil
}

// GetStoredDocument retrieves a stored document for test assertions.
func (dm *fakeDMServer) GetStoredDocument(docID string) *dmclient.Document {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	return dm.documents[docID]
}


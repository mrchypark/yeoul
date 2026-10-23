package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

func TestOpenAPIDocsAndSecurity(t *testing.T) {
	handler := NewHandler(testEngine(t), Config{BearerToken: "secret-token", RequireAuth: true, LoopbackOnly: true, ListenPort: "8080"})
	request := func(path, authorization, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://localhost:8080"+path, nil)
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}
	if got := request("/openapi.json", "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated OpenAPI status = %d", got.Code)
	}
	if got := request("/openapi.yaml", "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated OpenAPI YAML status = %d", got.Code)
	}
	if got := request("/docs", "Bearer secret-token", "https://attacker.test"); got.Code != http.StatusForbidden {
		t.Fatalf("cross-origin docs status = %d", got.Code)
	}
	if got := request("/docs", "Bearer secret-token", ""); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "Yeoul Service API") || !strings.Contains(got.Body.String(), `apiDescriptionUrl="/openapi.yaml"`) {
		t.Fatalf("docs response status = %d body=%s", got.Code, got.Body.String())
	}
	if got := request("/openapi.yaml", "Bearer secret-token", ""); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "openapi: 3.1.0") {
		t.Fatalf("OpenAPI YAML status = %d body=%s", got.Code, got.Body.String())
	}
	got := request("/openapi.json", "Bearer secret-token", "")
	if got.Code != http.StatusOK {
		t.Fatalf("OpenAPI status = %d body=%s", got.Code, got.Body.String())
	}
	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
			RequestBody struct {
				Content map[string]struct {
					Schema map[string]any `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
			Responses map[string]struct {
				Content map[string]struct {
					Schema map[string]any `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
			Security []map[string][]string `json:"security"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Type  json.RawMessage `json:"type"`
					Ref   string          `json:"$ref"`
					AnyOf []struct {
						Type string `json:"type"`
						Ref  string `json:"$ref"`
					} `json:"anyOf"`
				} `json:"properties"`
				Required []string `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Body.String(), `"$schema"`) || strings.Contains(got.Body.String(), "application/problem+json") {
		t.Fatal("OpenAPI advertises Huma-only schema links or Problem Details")
	}
	for _, route := range []struct{ path, method string }{
		{"/v1/health", "get"}, {"/v1/schema", "get"}, {"/v1/episodes", "post"},
		{"/v1/entities/{id}", "put"}, {"/v1/facts", "post"}, {"/v1/facts/{id}/supersede", "post"},
		{"/v1/facts/{id}/retract", "post"}, {"/v1/episodes/{id}", "get"}, {"/v1/entities/{id}", "get"},
		{"/v1/facts/{id}", "get"}, {"/v1/sources/{id}", "get"}, {"/v1/query/search", "post"},
		{"/v1/query/facts", "post"}, {"/v1/query/neighborhood", "post"}, {"/v1/query/timeline", "post"},
		{"/v1/query/provenance", "post"},
	} {
		op, ok := spec.Paths[route.path][route.method]
		if !ok || op.OperationID == "" {
			t.Errorf("missing operation %s %s", route.method, route.path)
			continue
		}
		if route.method == "post" || route.method == "put" {
			if len(op.RequestBody.Content["application/json"].Schema) == 0 {
				t.Errorf("missing typed request schema for %s", route.path)
			}
		}
		if route.path != "/v1/facts" && len(op.Responses["200"].Content["application/json"].Schema) == 0 {
			t.Errorf("missing typed success envelope for %s; schema=%v", route.path, spec.Paths[route.path][route.method].Responses["200"].Content)
		}
		if len(op.Responses["400"].Content["application/json"].Schema) == 0 {
			t.Errorf("missing error envelope for %s", route.path)
		}
		_, hasBearer := map[string][]string{}, false
		if len(op.Security) == 1 {
			_, hasBearer = op.Security[0]["BearerAuth"]
		}
		if !hasBearer || len(op.Security) != 1 {
			t.Errorf("required bearer security is inaccurate for %s: %#v", route.path, op.Security)
		}
	}
	for _, path := range []string{"/v1/episodes", "/v1/facts"} {
		post := spec.Paths[path]["post"]
		if len(post.Responses["201"].Content["application/json"].Schema) == 0 {
			t.Errorf("missing typed 201 response for %s", path)
		}
		if path == "/v1/facts" && post.Responses["200"].Content != nil {
			t.Error("fact assertion should not document a 200 response")
		}
		if path == "/v1/episodes" && len(post.Responses["200"].Content["application/json"].Schema) == 0 {
			t.Error("episode ingestion must document its replay 200 response")
		}
	}

	public := NewHandler(testEngine(t), Config{}).ServeHTTP
	publicReq := httptest.NewRequest(http.MethodGet, "http://example.test/openapi.json", nil)
	publicRec := httptest.NewRecorder()
	public(publicRec, publicReq)
	var publicSpec struct {
		Paths map[string]map[string]struct {
			Security []map[string][]string `json:"security"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(publicRec.Body.Bytes(), &publicSpec); err != nil {
		t.Fatal(err)
	}
	publicSecurity := publicSpec.Paths["/v1/query/search"]["post"].Security
	if len(publicSecurity) != 2 || len(publicSecurity[1]) != 0 {
		t.Fatalf("optional bearer security = %#v", publicSecurity)
	}

	for _, path := range []string{"/v1/health", "/v1/query/search"} {
		method := "post"
		if path == "/v1/health" {
			method = "get"
		}
		op := spec.Paths[path][method]
		for _, status := range []string{"200", "400"} {
			ref, _ := op.Responses[status].Content["application/json"].Schema["$ref"].(string)
			name := strings.TrimPrefix(ref, "#/components/schemas/")
			envelopeSchema, ok := spec.Components.Schemas[name]
			if !ok {
				t.Fatalf("missing envelope schema %q for %s %s", name, path, status)
			}
			for _, field := range []string{"data", "error"} {
				alternatives := envelopeSchema.Properties[field].AnyOf
				hasNull, hasObject := false, false
				for _, alternative := range alternatives {
					hasNull = hasNull || alternative.Type == "null"
					hasObject = hasObject || alternative.Ref != "" || alternative.Type == "object"
				}
				if !hasNull || !hasObject {
					t.Errorf("%s %s envelope %s schema is not object-or-null: %#v", path, status, field, alternatives)
				}
			}
		}
	}

	for _, tc := range []struct {
		method, path, body string
		wantStatus         int
		wantData           bool
	}{
		{http.MethodGet, "/v1/health", "", http.StatusOK, true},
		{http.MethodPost, "/v1/query/search", `{"query_text":"x","page":{"cursor":"bad"}}`, http.StatusBadRequest, false},
	} {
		req := httptest.NewRequest(tc.method, "http://localhost:8080"+tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer secret-token")
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		var payload map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if rr.Code != tc.wantStatus || (payload["data"] != nil) != tc.wantData || (payload["error"] == nil) != tc.wantData {
			t.Errorf("%s %s actual envelope mismatch: status=%d body=%s", tc.method, tc.path, rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), `"$schema"`) || strings.Contains(rr.Header().Get("Content-Type"), "problem+json") {
			t.Errorf("%s %s returned a Huma-only response shape", tc.method, tc.path)
		}
	}
}

func TestInvalidQueryCursorsAreBadRequests(t *testing.T) {
	handler := NewHandler(testEngine(t), Config{})
	for _, route := range []string{"search", "facts", "timeline"} {
		t.Run(route, func(t *testing.T) {
			body := `{"query_text":"x","page":{"cursor":"bad"}}`
			if route == "facts" {
				body = `{"page":{"cursor":"bad"}}`
			}
			if route == "timeline" {
				body = `{"page":{"cursor":"bad"}}`
			}
			req := httptest.NewRequest(http.MethodPost, "http://example.test/v1/query/"+route, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			var got envelope
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if rr.Code != http.StatusBadRequest || got.Error == nil || got.Error.Code != "invalid_argument" {
				t.Fatalf("status=%d envelope=%s", rr.Code, rr.Body.String())
			}
			if got.Error.Message != "page cursor is invalid" {
				t.Fatalf("error message = %q", got.Error.Message)
			}
		})
	}
}

func testEngine(t *testing.T) yeoul.Engine {
	t.Helper()
	engine, err := yeoul.Open(context.Background(), yeoul.Config{InMemory: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := engine.Close(context.Background()); err != nil {
			t.Errorf("close engine: %v", err)
		}
	})
	return engine
}

func request(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Request-ID", "req_test-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func decodeEnvelope(t *testing.T, response *httptest.ResponseRecorder) envelope {
	t.Helper()
	var got envelope
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode envelope: %v; body=%s", err, response.Body.String())
	}
	return got
}

func TestIngestAndGetUseV1Envelope(t *testing.T) {
	engine := testEngine(t)
	handler := NewHandler(engine, Config{LoopbackOnly: true, ListenPort: "8080"})
	created := request(handler, http.MethodPost, "http://127.0.0.1:8080/v1/episodes", "{\"space_id\":\"project:test\",\"episode\":{\"id\":\"ep-http\",\"kind\":\"note\",\"content\":\"from HTTP\"}}")
	if created.Code != http.StatusCreated {
		t.Fatalf("ingest status = %d, body=%s", created.Code, created.Body.String())
	}
	createdEnvelope := decodeEnvelope(t, created)
	if createdEnvelope.Meta.RequestID != "req_test-1" || createdEnvelope.Meta.SpaceID != "project:test" || createdEnvelope.Error != nil {
		t.Fatalf("ingest envelope = %#v", createdEnvelope)
	}

	got := request(handler, http.MethodGet, "http://127.0.0.1:8080/v1/episodes/ep-http?space_id=project%3Atest", "")
	if got.Code != http.StatusOK {
		t.Fatalf("get status = %d, body=%s", got.Code, got.Body.String())
	}
	body := decodeEnvelope(t, got)
	if body.Meta.SpaceID != "project:test" || body.Data == nil || body.Error != nil {
		t.Fatalf("get envelope = %#v", body)
	}
	wrongSpace := request(handler, http.MethodGet, "http://127.0.0.1:8080/v1/episodes/ep-http?space_id=other", "")
	if wrongSpace.Code != http.StatusNotFound || decodeEnvelope(t, wrongSpace).Error.Code != "not_found" {
		t.Fatalf("wrong-space status/body = %d %s", wrongSpace.Code, wrongSpace.Body.String())
	}
	if got.Header().Get("X-Request-ID") != "req_test-1" || got.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected response headers: %#v", got.Header())
	}
	if got.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing nosniff header: %#v", got.Header())
	}

	missingEpisode := request(handler, http.MethodGet, "http://127.0.0.1:8080/v1/episodes/missing", "")
	if missingEpisode.Code != http.StatusNotFound || decodeEnvelope(t, missingEpisode).Error.Code != "not_found" {
		t.Fatalf("missing episode status/body = %d %s", missingEpisode.Code, missingEpisode.Body.String())
	}
	_, err := engine.IngestEpisode(context.Background(), yeoul.EpisodeInput{ID: "ep/name", Kind: "note", Content: "slash id"})
	if err != nil {
		t.Fatal(err)
	}
	gotSlash := request(handler, http.MethodGet, "http://127.0.0.1:8080/v1/episodes/ep%2Fname", "")
	if gotSlash.Code != http.StatusOK {
		t.Fatalf("escaped slash id status/body = %d %s", gotSlash.Code, gotSlash.Body.String())
	}
}

func TestQueryBridgeAndEngineErrorMapping(t *testing.T) {
	engine := testEngine(t)
	if _, err := engine.IngestEpisode(context.Background(), yeoul.EpisodeInput{ID: "ep-query", Kind: "note", Content: "alpha beta"}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(engine, Config{})
	response := request(handler, http.MethodPost, "http://example.test/v1/query/search", "{\"meta\":{\"space_id\":\"default\"},\"query_text\":\"alpha\",\"types\":[\"episode\"]}")
	if response.Code != http.StatusOK {
		t.Fatalf("search status = %d, body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Meta struct {
			RequestID string `json:"request_id"`
		} `json:"meta"`
		Data struct {
			Hits []yeoul.SearchHit `json:"hits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Meta.RequestID != "req_test-1" || len(result.Data.Hits) != 1 || result.Data.Hits[0].RecordID != "ep-query" {
		t.Fatalf("search response = %#v", result)
	}

	missing := request(handler, http.MethodGet, "http://example.test/v1/facts/no-such-fact", "")
	if missing.Code != http.StatusNotFound || decodeEnvelope(t, missing).Error.Code != "not_found" {
		t.Fatalf("missing fact status/body = %d %s", missing.Code, missing.Body.String())
	}
}

func TestStrictJSONContentTypeAndBodyLimit(t *testing.T) {
	handler := NewHandler(testEngine(t), Config{})
	for _, tc := range []struct{ name, body, want string }{
		{"unknown field", "{\"episode\":{\"kind\":\"note\",\"content\":\"x\",\"extra\":true}}", "invalid_argument"},
		{"trailing value", "{\"episode\":{\"kind\":\"note\",\"content\":\"x\"}} {}", "invalid_argument"},
		{"duplicate key", "{\"space_id\":\"a\",\"space_id\":\"b\",\"episode\":{\"kind\":\"note\",\"content\":\"x\"}}", "invalid_argument"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := request(handler, http.MethodPost, "http://example.test/v1/episodes", tc.body)
			if response.Code != http.StatusBadRequest || decodeEnvelope(t, response).Error.Code != tc.want {
				t.Fatalf("status/body = %d %s", response.Code, response.Body.String())
			}
		})
	}

	noContentType := httptest.NewRequest(http.MethodPost, "/v1/episodes", strings.NewReader("{}"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, noContentType)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("missing content type status = %d", response.Code)
	}

	oversized := strings.Repeat(" ", maxRequestBody+1)
	response = request(handler, http.MethodPost, "http://example.test/v1/episodes", oversized)
	if response.Code != http.StatusRequestEntityTooLarge || decodeEnvelope(t, response).Error.Code != "request_too_large" {
		t.Fatalf("oversize status/body = %d %s", response.Code, response.Body.String())
	}
}

func TestOriginHostAndBearerGuards(t *testing.T) {
	handler := NewHandler(testEngine(t), Config{BearerToken: "secret-token", RequireAuth: true, LoopbackOnly: true, ListenPort: "8080"})
	newRequest := func(host, origin, authorization string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+"/v1/health", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	if got := newRequest("attacker.test:8080", "", ""); got.Code != http.StatusForbidden {
		t.Fatalf("DNS-rebinding host status = %d", got.Code)
	}
	if got := newRequest("localhost:8080", "http://attacker.test", "Bearer secret-token"); got.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", got.Code)
	}
	if got := newRequest("localhost:8080", "http://localhost:8080/path", "Bearer secret-token"); got.Code != http.StatusForbidden {
		t.Fatalf("non-origin URL status = %d", got.Code)
	}
	if got := newRequest("localhost:8080", "", "Bearer wrong"); got.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-token status = %d", got.Code)
	}
	if got := newRequest("localhost:8080", "http://localhost:8080", "Bearer secret-token"); got.Code != http.StatusOK {
		t.Fatalf("authorized same-origin status = %d, body=%s", got.Code, got.Body.String())
	} else if bytes.Contains(got.Body.Bytes(), []byte("secret-token")) {
		t.Fatal("response exposed bearer token")
	}
}

func TestUnsupportedIdempotencyHeaderAndEntityUpsert(t *testing.T) {
	handler := NewHandler(testEngine(t), Config{})
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Idempotency-Key", "client-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest || decodeEnvelope(t, response).Error.Code != "unsupported_header" {
		t.Fatalf("idempotency status/body = %d %s", response.Code, response.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header[http.CanonicalHeaderKey("Idempotency-Key")] = []string{""}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest || decodeEnvelope(t, response).Error.Code != "unsupported_header" {
		t.Fatalf("empty idempotency status/body = %d %s", response.Code, response.Body.String())
	}

	response = request(handler, http.MethodPut, "http://example.test/v1/entities/entity-1", "{\"space_id\":\"default\",\"entity\":{\"type\":\"Project\",\"canonical_name\":\"Yeoul\"}}")
	if response.Code != http.StatusOK || decodeEnvelope(t, response).Data == nil {
		t.Fatalf("upsert status/body = %d %s", response.Code, response.Body.String())
	}
	mismatch := request(handler, http.MethodPut, "http://example.test/v1/entities/entity-1", "{\"entity\":{\"id\":\"other\",\"type\":\"Project\",\"canonical_name\":\"Yeoul\"}}")
	if mismatch.Code != http.StatusBadRequest {
		t.Fatalf("mismatched id status = %d", mismatch.Code)
	}
}

func TestFactLifecycleUsesSpaceReasonAndEncodedSlashIDs(t *testing.T) {
	engine := testEngine(t)
	_, err := engine.UpsertEntity(context.Background(), yeoul.EntityInput{ID: "project:scope", SpaceID: "team-a", Type: "Project", CanonicalName: "Scoped"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.IngestEpisode(context.Background(), yeoul.EpisodeInput{ID: "ep:scope", SpaceID: "team-a", Kind: "note", Content: "support"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(engine, Config{})

	created := request(handler, http.MethodPost, "http://example.test/v1/facts", "{\"space_id\":\"team-a\",\"fact\":{\"id\":\"fact:old/one\",\"predicate\":\"HAS_STATE\",\"subject_id\":\"project:scope\",\"value_text\":\"old\",\"supporting_episode_ids\":[\"ep:scope\"]}}")
	if created.Code != http.StatusCreated {
		t.Fatalf("assert status/body = %d %s", created.Code, created.Body.String())
	}
	var asserted struct {
		Data yeoul.Fact `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &asserted); err != nil {
		t.Fatal(err)
	}
	if asserted.Data.Status != "active" || asserted.Data.SpaceID != "team-a" {
		t.Fatalf("asserted fact = %+v", asserted.Data)
	}

	wrongScope := request(handler, http.MethodPost, "http://example.test/v1/facts/fact:old%2Fone/supersede", "{\"space_id\":\"other\",\"new_fact\":{\"id\":\"fact:wrong\",\"predicate\":\"HAS_STATE\",\"subject_id\":\"project:scope\",\"value_text\":\"wrong\",\"supporting_episode_ids\":[\"ep:scope\"]},\"reason\":\"wrong scope\"}")
	if wrongScope.Code != http.StatusNotFound {
		t.Fatalf("wrong-scope supersede status/body = %d %s", wrongScope.Code, wrongScope.Body.String())
	}

	superseded := request(handler, http.MethodPost, "http://example.test/v1/facts/fact:old%2Fone/supersede", "{\"space_id\":\"team-a\",\"new_fact\":{\"id\":\"fact:new/one\",\"predicate\":\"HAS_STATE\",\"subject_id\":\"project:scope\",\"value_text\":\"new\",\"supporting_episode_ids\":[\"ep:scope\"]},\"reason\":\"confirmed update\"}")
	if superseded.Code != http.StatusOK {
		t.Fatalf("supersede status/body = %d %s", superseded.Code, superseded.Body.String())
	}
	oldFact, err := engine.GetFact(context.Background(), asserted.Data.ID)
	if err != nil {
		t.Fatal(err)
	}
	if oldFact.Status != "superseded" || oldFact.Metadata["supersede_reason"] != "confirmed update" {
		t.Fatalf("superseded fact = %+v", oldFact)
	}

	retracted := request(handler, http.MethodPost, "http://example.test/v1/facts/fact:new%2Fone/retract", "{\"space_id\":\"team-a\",\"reason\":\"source withdrawn\"}")
	if retracted.Code != http.StatusOK {
		t.Fatalf("retract status/body = %d %s", retracted.Code, retracted.Body.String())
	}
	newFact, err := engine.GetFact(context.Background(), "fact:new/one")
	if err != nil {
		t.Fatal(err)
	}
	if newFact.Status != "retracted" || newFact.RetractionReason != "source withdrawn" || newFact.SpaceID != "team-a" {
		t.Fatalf("retracted fact = %+v", newFact)
	}
}

func TestDuplicateKeysNestedAndEscaped(t *testing.T) {
	for _, raw := range []string{`{"meta":{"space_id":"a","space_id":"b"}}`, `{"a\u0062":1,"ab":2}`} {
		if err := rejectDuplicateKeys([]byte(raw)); err == nil {
			t.Fatalf("duplicate keys accepted: %s", raw)
		}
	}
}

func TestRhizaHTTPPersistence(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "memory")
	engine, err := yeoul.Open(context.Background(), yeoul.Config{
		Driver:          yeoul.StorageDriverRhiza,
		DatabasePath:    databasePath,
		CreateIfMissing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(engine, Config{})
	created := request(handler, http.MethodPost, "http://example.test/v1/episodes", `{"space_id":"default","episode":{"id":"rhiza-http","kind":"note","content":"persisted through the service"}}`)
	if created.Code != http.StatusCreated {
		_ = engine.Close(context.Background())
		t.Fatalf("ingest status/body = %d %s", created.Code, created.Body.String())
	}
	if err := engine.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	reopened, err := yeoul.Open(context.Background(), yeoul.Config{Driver: yeoul.StorageDriverRhiza, DatabasePath: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(context.Background()); err != nil {
			t.Errorf("close reopened Rhiza engine: %v", err)
		}
	})
	got := request(NewHandler(reopened, Config{}), http.MethodGet, "http://example.test/v1/episodes/rhiza-http?space_id=default", "")
	if got.Code != http.StatusOK {
		t.Fatalf("reopened get status/body = %d %s", got.Code, got.Body.String())
	}
}

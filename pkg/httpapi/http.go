package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

const maxRequestBody = 1 << 20

type Config struct {
	BearerToken  string
	RequireAuth  bool
	LoopbackOnly bool
	ListenPort   string
}

type apiError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type responseMeta struct {
	RequestID   string     `json:"request_id"`
	SpaceID     string     `json:"space_id,omitempty"`
	SnapshotAt  *time.Time `json:"snapshot_at,omitempty"`
	NextCursor  string     `json:"next_cursor,omitempty"`
	TotalApprox *int64     `json:"total_approx,omitempty"`
}

type envelope struct {
	Meta  responseMeta `json:"meta"`
	Data  any          `json:"data"`
	Error *apiError    `json:"error"`
}

type handler struct {
	engine yeoul.Engine
	cfg    Config
	docs   http.Handler
}

func NewHandler(engine yeoul.Engine, cfg Config) http.Handler {
	h := &handler{engine: engine, cfg: cfg, docs: newDocsHandler(cfg.RequireAuth)}
	return http.HandlerFunc(h.serveHTTP)
}

func (h *handler) serveHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := requestID(r.Header.Get("X-Request-ID"))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Request-ID", requestID)

	if _, exists := r.Header[http.CanonicalHeaderKey("Idempotency-Key")]; exists {
		h.writeError(w, requestID, "unsupported_header", "Idempotency-Key is not supported", nil, http.StatusBadRequest)
		return
	}
	if !h.safeBrowserRequest(r) {
		h.writeError(w, requestID, "invalid_origin", "request origin or host is not allowed", nil, http.StatusForbidden)
		return
	}
	if h.cfg.RequireAuth && !h.authorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer`)
		h.writeError(w, requestID, "unauthorized", "bearer authentication is required", nil, http.StatusUnauthorized)
		return
	}
	if path := strings.TrimSuffix(r.URL.EscapedPath(), "/"); r.Method == http.MethodGet && (path == "/docs" || path == "/openapi.json" || path == "/openapi.yaml") {
		h.docs.ServeHTTP(w, r)
		return
	}
	if h.engine == nil {
		h.writeError(w, requestID, "storage_failure", "database is not ready", nil, http.StatusServiceUnavailable)
		return
	}

	path := strings.TrimSuffix(r.URL.EscapedPath(), "/")
	if path == "" {
		path = "/"
	}
	if r.Method == http.MethodGet && path == "/v1/health" {
		if _, err := h.engine.GetRecord(r.Context(), yeoul.GetRecordRequest{Kind: "fact", ID: "__yeoul_health_probe__"}); err != nil {
			var coreErr *yeoul.Error
			if !errors.As(err, &coreErr) || coreErr.Code != yeoul.ErrFactNotFound {
				h.writeError(w, requestID, "storage_failure", "database is not ready", nil, http.StatusServiceUnavailable)
				return
			}
		}
		h.writeData(w, requestID, "", http.StatusOK, map[string]string{"status": "ok", "service": "yeould", "database": "ready"})
		return
	}
	if r.Method == http.MethodGet && path == "/v1/schema" {
		h.writeData(w, requestID, "", http.StatusOK, map[string]string{"api_version": "v1"})
		return
	}
	if r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/") {
		if h.getRecord(w, r, requestID, path) {
			return
		}
	}
	if r.Method == http.MethodPut && strings.HasPrefix(path, "/v1/entities/") {
		if h.upsertEntity(w, r, requestID, path) {
			return
		}
	}
	if r.Method == http.MethodPost {
		switch path {
		case "/v1/episodes":
			h.ingestEpisode(w, r, requestID)
			return
		case "/v1/facts":
			h.assertFact(w, r, requestID)
			return
		case "/v1/query/search":
			h.query(w, r, requestID, func(ctx context.Context, body []byte) (any, string, error) {
				var req yeoul.SearchRequest
				if err := decodeJSON(body, &req); err != nil {
					return nil, "", err
				}
				req.Meta.RequestID = requestID
				result, err := h.engine.Search(ctx, req)
				return result, req.Meta.SpaceID, err
			})
			return
		case "/v1/query/facts":
			h.query(w, r, requestID, func(ctx context.Context, body []byte) (any, string, error) {
				var req yeoul.FactLookupRequest
				if err := decodeJSON(body, &req); err != nil {
					return nil, "", err
				}
				req.Meta.RequestID = requestID
				result, err := h.engine.LookupFacts(ctx, req)
				return result, req.Meta.SpaceID, err
			})
			return
		case "/v1/query/neighborhood":
			h.query(w, r, requestID, func(ctx context.Context, body []byte) (any, string, error) {
				var req yeoul.NeighborhoodRequest
				if err := decodeJSON(body, &req); err != nil {
					return nil, "", err
				}
				req.Meta.RequestID = requestID
				result, err := h.engine.Neighborhood(ctx, req)
				return result, req.Meta.SpaceID, err
			})
			return
		case "/v1/query/timeline":
			h.query(w, r, requestID, func(ctx context.Context, body []byte) (any, string, error) {
				var req yeoul.TimelineRequest
				if err := decodeJSON(body, &req); err != nil {
					return nil, "", err
				}
				req.Meta.RequestID = requestID
				result, err := h.engine.Timeline(ctx, req)
				return result, req.Meta.SpaceID, err
			})
			return
		case "/v1/query/provenance":
			h.query(w, r, requestID, func(ctx context.Context, body []byte) (any, string, error) {
				var req yeoul.ProvenanceRequest
				if err := decodeJSON(body, &req); err != nil {
					return nil, "", err
				}
				req.Meta.RequestID = requestID
				result, err := h.engine.Provenance(ctx, req)
				return result, req.Meta.SpaceID, err
			})
			return
		}
		if strings.HasPrefix(path, "/v1/facts/") {
			if h.factLifecycle(w, r, requestID, path) {
				return
			}
		}
	}
	h.writeError(w, requestID, "not_found", "route not found", nil, http.StatusNotFound)
}

type episodeBody struct {
	SpaceID string             `json:"space_id,omitempty"`
	Episode yeoul.EpisodeInput `json:"episode"`
}

func (h *handler) ingestEpisode(w http.ResponseWriter, r *http.Request, requestID string) {
	var body episodeBody
	if !h.readBody(w, r, requestID, &body) {
		return
	}
	if body.Episode.SpaceID != "" && body.SpaceID != "" && body.Episode.SpaceID != body.SpaceID {
		h.writeError(w, requestID, "invalid_argument", "space_id does not match episode.space_id", nil, http.StatusBadRequest)
		return
	}
	if body.Episode.SpaceID == "" {
		body.Episode.SpaceID = body.SpaceID
	}
	result, err := h.engine.IngestEpisode(r.Context(), body.Episode)
	if err != nil {
		h.writeCoreError(w, requestID, body.Episode.SpaceID, err)
		return
	}
	status := http.StatusCreated
	if !result.Created {
		status = http.StatusOK
	}
	h.writeData(w, requestID, body.Episode.SpaceID, status, result)
}

type entityBody struct {
	SpaceID string            `json:"space_id,omitempty"`
	Entity  yeoul.EntityInput `json:"entity"`
}

func (h *handler) upsertEntity(w http.ResponseWriter, r *http.Request, requestID, path string) bool {
	id, ok := pathID(path, "/v1/entities/")
	if !ok {
		return false
	}
	var body entityBody
	if !h.readBody(w, r, requestID, &body) {
		return true
	}
	if body.Entity.ID != "" && body.Entity.ID != id {
		h.writeError(w, requestID, "invalid_argument", "path id does not match entity.id", nil, http.StatusBadRequest)
		return true
	}
	if body.Entity.SpaceID != "" && body.SpaceID != "" && body.Entity.SpaceID != body.SpaceID {
		h.writeError(w, requestID, "invalid_argument", "space_id does not match entity.space_id", nil, http.StatusBadRequest)
		return true
	}
	body.Entity.ID = id
	if body.Entity.SpaceID == "" {
		body.Entity.SpaceID = body.SpaceID
	}
	entity, err := h.engine.UpsertEntity(r.Context(), body.Entity)
	if err != nil {
		h.writeCoreError(w, requestID, body.Entity.SpaceID, err)
		return true
	}
	h.writeData(w, requestID, body.Entity.SpaceID, http.StatusOK, entity)
	return true
}

type factBody struct {
	SpaceID string          `json:"space_id,omitempty"`
	Fact    yeoul.FactInput `json:"fact"`
}

func (h *handler) assertFact(w http.ResponseWriter, r *http.Request, requestID string) {
	var body factBody
	if !h.readBody(w, r, requestID, &body) {
		return
	}
	if !bridgeSpaceID(&body.Fact.SpaceID, body.SpaceID) {
		h.writeError(w, requestID, "invalid_argument", "space_id does not match fact.space_id", nil, http.StatusBadRequest)
		return
	}
	fact, err := h.engine.AssertFact(r.Context(), body.Fact)
	if err != nil {
		h.writeCoreError(w, requestID, body.Fact.SpaceID, err)
		return
	}
	h.writeData(w, requestID, body.Fact.SpaceID, http.StatusCreated, fact)
}

type supersedeBody struct {
	SpaceID string          `json:"space_id,omitempty"`
	NewFact yeoul.FactInput `json:"new_fact"`
	Reason  string          `json:"reason" minLength:"1" pattern:"\\S"`
}

type retractBody struct {
	SpaceID string `json:"space_id,omitempty"`
	Reason  string `json:"reason" minLength:"1" pattern:"\\S"`
}

func (h *handler) factLifecycle(w http.ResponseWriter, r *http.Request, requestID, path string) bool {
	rest := strings.TrimPrefix(path, "/v1/facts/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || (parts[1] != "supersede" && parts[1] != "retract") {
		return false
	}
	id, err := url.PathUnescape(parts[0])
	if err != nil || id == "" {
		return false
	}
	if parts[1] == "supersede" {
		var body supersedeBody
		if !h.readBody(w, r, requestID, &body) {
			return true
		}
		if strings.TrimSpace(body.Reason) == "" {
			h.writeError(w, requestID, "invalid_argument", "reason is required", nil, http.StatusBadRequest)
			return true
		}
		if !bridgeSpaceID(&body.NewFact.SpaceID, body.SpaceID) {
			h.writeError(w, requestID, "invalid_argument", "space_id does not match new_fact.space_id", nil, http.StatusBadRequest)
			return true
		}
		if body.SpaceID == "" {
			body.SpaceID = body.NewFact.SpaceID
		}
		if _, err := h.engine.GetRecord(r.Context(), yeoul.GetRecordRequest{Meta: yeoul.QueryMeta{SpaceID: body.SpaceID}, Kind: "fact", ID: id}); err != nil {
			h.writeCoreError(w, requestID, body.SpaceID, err)
			return true
		}
		result, err := h.engine.SupersedeFact(r.Context(), id, body.NewFact, body.Reason)
		if err != nil {
			h.writeCoreError(w, requestID, body.SpaceID, err)
			return true
		}
		h.writeData(w, requestID, body.SpaceID, http.StatusOK, result)
		return true
	}
	var body retractBody
	if !h.readBody(w, r, requestID, &body) {
		return true
	}
	if strings.TrimSpace(body.Reason) == "" {
		h.writeError(w, requestID, "invalid_argument", "reason is required", nil, http.StatusBadRequest)
		return true
	}
	if _, err := h.engine.GetRecord(r.Context(), yeoul.GetRecordRequest{Meta: yeoul.QueryMeta{SpaceID: body.SpaceID}, Kind: "fact", ID: id}); err != nil {
		h.writeCoreError(w, requestID, body.SpaceID, err)
		return true
	}
	result, err := h.engine.RetractFact(r.Context(), id, body.Reason)
	if err != nil {
		h.writeCoreError(w, requestID, body.SpaceID, err)
		return true
	}
	h.writeData(w, requestID, body.SpaceID, http.StatusOK, result)
	return true
}

func (h *handler) getRecord(w http.ResponseWriter, r *http.Request, requestID, path string) bool {
	parts := strings.Split(strings.TrimPrefix(path, "/v1/"), "/")
	if len(parts) != 2 {
		return false
	}
	kind := parts[0]
	if kind != "episodes" && kind != "entities" && kind != "facts" && kind != "sources" {
		return false
	}
	id, err := url.PathUnescape(parts[1])
	if err != nil || id == "" {
		return false
	}
	query, err := parseGetQuery(r.URL.Query())
	if err != nil {
		h.writeError(w, requestID, "invalid_argument", err.Error(), nil, http.StatusBadRequest)
		return true
	}
	if query.provenance || query.related || query.supporting {
		h.writeError(w, requestID, "unsupported", "record expansions are not supported", nil, http.StatusNotImplemented)
		return true
	}
	requestKind := map[string]string{"episodes": "episode", "entities": "entity", "facts": "fact", "sources": "source"}[kind]
	request := yeoul.GetRecordRequest{Meta: yeoul.QueryMeta{RequestID: requestID, SpaceID: query.spaceID}, Kind: requestKind, ID: id}
	if query.asOf != nil {
		request.Temporal.AsOf = query.asOf
	}
	request.Include.Provenance = query.provenance
	request.Include.RelatedEntities = query.related
	request.Include.SupportingEpisodes = query.supporting
	result, err := h.engine.GetRecord(r.Context(), request)
	if err != nil {
		h.writeCoreError(w, requestID, query.spaceID, err)
		return true
	}
	h.writeData(w, requestID, query.spaceID, http.StatusOK, result)
	return true
}

type getQuery struct {
	spaceID    string
	asOf       *time.Time
	provenance bool
	related    bool
	supporting bool
}

func parseGetQuery(values url.Values) (getQuery, error) {
	var result getQuery
	for key, entries := range values {
		if len(entries) != 1 {
			return result, fmt.Errorf("query parameter %s must appear once", key)
		}
		switch key {
		case "space_id":
			result.spaceID = entries[0]
		case "as_of":
			value, err := time.Parse(time.RFC3339Nano, entries[0])
			if err != nil {
				return result, fmt.Errorf("as_of must be RFC3339")
			}
			result.asOf = &value
		case "include_provenance":
			value, err := strconv.ParseBool(entries[0])
			if err != nil {
				return result, fmt.Errorf("include_provenance must be boolean")
			}
			result.provenance = value
		case "include_related_entities":
			value, err := strconv.ParseBool(entries[0])
			if err != nil {
				return result, fmt.Errorf("include_related_entities must be boolean")
			}
			result.related = value
		case "include_supporting_episodes":
			value, err := strconv.ParseBool(entries[0])
			if err != nil {
				return result, fmt.Errorf("include_supporting_episodes must be boolean")
			}
			result.supporting = value
		default:
			return result, fmt.Errorf("unknown query parameter %s", key)
		}
	}
	return result, nil
}

func (h *handler) query(w http.ResponseWriter, r *http.Request, requestID string, call func(context.Context, []byte) (any, string, error)) {
	body, ok := h.readRawBody(w, r, requestID)
	if !ok {
		return
	}
	data, spaceID, err := call(r.Context(), body)
	if err != nil {
		var bodyErr *requestBodyError
		if errors.As(err, &bodyErr) {
			h.writeError(w, requestID, "invalid_argument", "invalid JSON request body", map[string]any{"reason": bodyErr.Error()}, http.StatusBadRequest)
		} else {
			h.writeCoreError(w, requestID, spaceID, err)
		}
		return
	}
	h.writeData(w, requestID, spaceID, http.StatusOK, data)
}

func (h *handler) readBody(w http.ResponseWriter, r *http.Request, requestID string, target any) bool {
	body, ok := h.readRawBody(w, r, requestID)
	if !ok {
		return false
	}
	if err := decodeJSON(body, target); err != nil {
		if errors.Is(err, errBodyTooLarge) {
			h.writeError(w, requestID, "request_too_large", "request body exceeds the size limit", nil, http.StatusRequestEntityTooLarge)
		} else {
			h.writeError(w, requestID, "invalid_argument", "invalid JSON request body", map[string]any{"reason": err.Error()}, http.StatusBadRequest)
		}
		return false
	}
	return true
}

var errBodyTooLarge = errors.New("request body too large")

type requestBodyError struct{ error }

func (h *handler) readRawBody(w http.ResponseWriter, r *http.Request, requestID string) ([]byte, bool) {
	mediaType, _, err := mimeParse(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		h.writeError(w, requestID, "invalid_argument", "Content-Type must be application/json", nil, http.StatusUnsupportedMediaType)
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) || isMaxBytesError(err) {
			h.writeError(w, requestID, "request_too_large", "request body exceeds the size limit", nil, http.StatusRequestEntityTooLarge)
		} else {
			h.writeError(w, requestID, "invalid_argument", "could not read request body", nil, http.StatusBadRequest)
		}
		return nil, false
	}
	return body, true
}

func mimeParse(value string) (string, map[string]string, error) {
	mediaType, params, err := mime.ParseMediaType(value)
	return strings.ToLower(mediaType), params, err
}

func isMaxBytesError(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

func decodeJSON(body []byte, target any) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return &requestBodyError{errors.New("request body must be a JSON object")}
	}
	if err := rejectDuplicateKeys(body); err != nil {
		return &requestBodyError{err}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return &requestBodyError{err}
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return &requestBodyError{errors.New("multiple JSON values")}
		}
		return &requestBodyError{err}
	}
	return nil
}

func rejectDuplicateKeys(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid object key")
			}
			folded := strings.ToLower(key)
			if _, exists := seen[folded]; exists {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[folded] = struct{}{}
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return errors.New("invalid JSON delimiter")
	}
}

func bridgeSpaceID(inner *string, outer string) bool {
	if *inner != "" && outer != "" && *inner != outer {
		return false
	}
	if *inner == "" {
		*inner = outer
	}
	return true
}

func pathID(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	encoded := strings.TrimPrefix(path, prefix)
	if encoded == "" || strings.Contains(encoded, "/") {
		return "", false
	}
	id, err := url.PathUnescape(encoded)
	return id, err == nil && id != ""
}

func (h *handler) safeBrowserRequest(r *http.Request) bool {
	host := strings.ToLower(r.Host)
	if host == "" {
		return false
	}
	if h.cfg.LoopbackOnly {
		hostname, port, err := net.SplitHostPort(host)
		if err != nil {
			hostname = strings.Trim(host, "[]")
			port = ""
		}
		ip := net.ParseIP(hostname)
		if hostname != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return false
		}
		if h.cfg.ListenPort != "" && port != h.cfg.ListenPort {
			return false
		}
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.EqualFold(parsed.Host, r.Host) {
			return false
		}
	}
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	return true
}

func (h *handler) authorized(r *http.Request) bool {
	if h.cfg.BearerToken == "" {
		return false
	}
	value := r.Header.Get("Authorization")
	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	provided := strings.TrimLeft(parts[1], " ")
	providedHash := sha256.Sum256([]byte(provided))
	tokenHash := sha256.Sum256([]byte(h.cfg.BearerToken))
	return subtle.ConstantTimeCompare(providedHash[:], tokenHash[:]) == 1
}

func (h *handler) writeCoreError(w http.ResponseWriter, requestID, spaceID string, err error) {
	var coreErr *yeoul.Error
	if !errors.As(err, &coreErr) {
		h.writeError(w, requestID, "internal", "internal server error", nil, http.StatusInternalServerError)
		return
	}
	code, status, message := "internal", http.StatusInternalServerError, "internal server error"
	details := coreErr.Details
	switch coreErr.Code {
	case yeoul.ErrConfigInvalid, yeoul.ErrInputInvalid:
		code, status, message = "invalid_argument", http.StatusBadRequest, coreErr.Message
	case yeoul.ErrEntityNotFound, yeoul.ErrFactNotFound, yeoul.ErrSourceNotFound:
		code, status, message = "not_found", http.StatusNotFound, coreErr.Message
	case yeoul.ErrFactConflict:
		code, status, message = "fact_conflict", http.StatusConflict, coreErr.Message
	case yeoul.ErrLifecycleInvalid, yeoul.ErrEntityNearDuplicate:
		code, status, message = "already_exists", http.StatusConflict, coreErr.Message
	case yeoul.ErrNotSupported:
		code, status, message, details = "unsupported", http.StatusNotImplemented, coreErr.Message, nil
	case yeoul.ErrStorageFailed:
		code, status, message, details = "storage_failure", http.StatusServiceUnavailable, "storage unavailable", nil
	case yeoul.ErrQueryFailed:
		switch coreErr.Message {
		case "record not found in space", "record not visible at requested time", "episode not found":
			code, status, message, details = "not_found", http.StatusNotFound, "record not found", nil
		default:
			code, status, message, details = "internal", http.StatusInternalServerError, "query failed", nil
		}
	}
	h.writeErrorWithSpace(w, requestID, spaceID, code, message, details, status)
}

func (h *handler) writeData(w http.ResponseWriter, requestID, spaceID string, status int, data any) {
	meta := responseMeta{RequestID: requestID, SpaceID: spaceID}
	switch value := data.(type) {
	case *yeoul.GetRecordResponse:
		meta = mergeMeta(meta, value.Meta)
	case *yeoul.SearchResponse:
		meta = mergeMeta(meta, value.Meta)
	case *yeoul.FactLookupResponse:
		meta = mergeMeta(meta, value.Meta)
	case *yeoul.NeighborhoodResponse:
		meta = mergeMeta(meta, value.Meta)
	case *yeoul.TimelineResponse:
		meta = mergeMeta(meta, value.Meta)
	case *yeoul.ProvenanceResponse:
		meta = mergeMeta(meta, value.Meta)
	}
	meta.RequestID = requestID
	if meta.SpaceID == "" {
		meta.SpaceID = spaceID
	}
	h.write(w, status, envelope{Meta: meta, Data: data})
}

func mergeMeta(base responseMeta, meta yeoul.QueryResponseMeta) responseMeta {
	base.SpaceID = meta.SpaceID
	base.SnapshotAt = meta.SnapshotAt
	base.NextCursor = meta.NextCursor
	base.TotalApprox = meta.TotalApprox
	return base
}

func (h *handler) writeError(w http.ResponseWriter, requestID, code, message string, details map[string]any, status int) {
	h.writeErrorWithSpace(w, requestID, "", code, message, details, status)
}

func (h *handler) writeErrorWithSpace(w http.ResponseWriter, requestID, spaceID, code, message string, details map[string]any, status int) {
	h.write(w, status, envelope{Meta: responseMeta{RequestID: requestID, SpaceID: spaceID}, Error: &apiError{Code: code, Message: message, Details: details}})
}

func (h *handler) write(w http.ResponseWriter, status int, response envelope) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func requestID(value string) string {
	if len(value) > 128 || value == "" {
		return newRequestID()
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
			return newRequestID()
		}
	}
	return value
}

func newRequestID() string {
	var data [12]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "req_unknown"
	}
	return "req_" + hex.EncodeToString(data[:])
}

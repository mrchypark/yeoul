package httpapi

import (
	"context"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/mrchypark/yeoul/pkg/yeoul"
)

type documentedEnvelope[T any] struct {
	Meta  responseMeta             `json:"meta"`
	Data  nullableSchema[T]        `json:"data"`
	Error nullableSchema[apiError] `json:"error"`
}

type documentedErrorEnvelope struct {
	Meta  responseMeta             `json:"meta"`
	Data  nullableObject           `json:"data"`
	Error nullableSchema[apiError] `json:"error"`
}

type nullableObject struct{}

func (nullableObject) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{AnyOf: []*huma.Schema{{Type: "object", AdditionalProperties: true}, {Type: "null"}}}
}

type nullableSchema[T any] struct{}

func (nullableSchema[T]) Schema(r huma.Registry) *huma.Schema {
	return &huma.Schema{AnyOf: []*huma.Schema{r.Schema(reflect.TypeFor[T](), true, ""), {Type: "null"}}}
}

type docOutput[T any] struct {
	Body documentedEnvelope[T] `body:""`
}

type docEpisodeInput struct {
	Body episodeBody `body:""`
}
type docEntityInput struct {
	ID   string     `path:"id"`
	Body entityBody `body:""`
}
type docFactInput struct {
	Body factBody `body:""`
}
type docSupersedeInput struct {
	ID   string        `path:"id"`
	Body supersedeBody `body:""`
}
type docRetractInput struct {
	ID   string      `path:"id"`
	Body retractBody `body:""`
}
type docGetRecordInput struct {
	ID      string `path:"id"`
	SpaceID string `query:"space_id"`
	AsOf    string `query:"as_of" format:"date-time"`
}
type docSearchInput struct {
	Body yeoul.SearchRequest `body:""`
}
type docFactsInput struct {
	Body yeoul.FactLookupRequest `body:""`
}
type docNeighborhoodInput struct {
	Body yeoul.NeighborhoodRequest `body:""`
}
type docTimelineInput struct {
	Body yeoul.TimelineRequest `body:""`
}
type docProvenanceInput struct {
	Body yeoul.ProvenanceRequest `body:""`
}
type healthData struct {
	Status   string `json:"status"`
	Service  string `json:"service"`
	Database string `json:"database"`
}
type schemaData struct {
	APIVersion string `json:"api_version"`
}

func newDocsHandler(requireAuth bool) http.Handler {
	mux := http.NewServeMux()
	config := huma.DefaultConfig("Yeoul Service API", "v1")
	config.SchemasPath = ""
	config.CreateHooks = nil
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"BearerAuth": {Type: "http", Scheme: "bearer", Description: "Required for non-loopback binds; optional on loopback unless YEOUL_HTTP_TOKEN is configured."},
	}
	api := humago.New(mux, config)
	security := []map[string][]string{{"BearerAuth": {}}}
	if !requireAuth {
		security = append(security, map[string][]string{})
	}
	registerDoc[struct{}, docOutput[healthData]](api, "get-health", http.MethodGet, "/v1/health", security)
	registerDoc[struct{}, docOutput[schemaData]](api, "get-schema", http.MethodGet, "/v1/schema", security)
	registerDoc[docEpisodeInput, docOutput[yeoul.EpisodeResult]](api, "ingest-episode", http.MethodPost, "/v1/episodes", security)
	addResponse(api, "/v1/episodes", "201", "Created")
	registerDoc[docEntityInput, docOutput[yeoul.Entity]](api, "upsert-entity", http.MethodPut, "/v1/entities/{id}", security)
	registerDoc[docFactInput, docOutput[yeoul.Fact]](api, "assert-fact", http.MethodPost, "/v1/facts", security)
	setFactSupportSchema(api)
	addResponse(api, "/v1/facts", "201", "Created")
	delete(api.OpenAPI().Paths["/v1/facts"].Post.Responses, "200")
	registerDoc[docSupersedeInput, docOutput[yeoul.SupersedeFactResult]](api, "supersede-fact", http.MethodPost, "/v1/facts/{id}/supersede", security)
	registerDoc[docRetractInput, docOutput[yeoul.RetractFactResult]](api, "retract-fact", http.MethodPost, "/v1/facts/{id}/retract", security)
	for _, kind := range []string{"episodes", "entities", "facts", "sources"} {
		registerDoc[docGetRecordInput, docOutput[yeoul.GetRecordResponse]](api, "get-"+kind, http.MethodGet, "/v1/"+kind+"/{id}", security)
	}
	registerDoc[docSearchInput, docOutput[yeoul.SearchResponse]](api, "search", http.MethodPost, "/v1/query/search", security)
	registerDoc[docFactsInput, docOutput[yeoul.FactLookupResponse]](api, "lookup-facts", http.MethodPost, "/v1/query/facts", security)
	registerDoc[docNeighborhoodInput, docOutput[yeoul.NeighborhoodResponse]](api, "get-neighborhood", http.MethodPost, "/v1/query/neighborhood", security)
	registerDoc[docTimelineInput, docOutput[yeoul.TimelineResponse]](api, "get-timeline", http.MethodPost, "/v1/query/timeline", security)
	registerDoc[docProvenanceInput, docOutput[yeoul.ProvenanceResponse]](api, "get-provenance", http.MethodPost, "/v1/query/provenance", security)
	return mux
}

func setFactSupportSchema(api huma.API) {
	ref := api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[yeoul.FactInput](), true, "").Ref
	schema := api.OpenAPI().Components.Schemas.SchemaFromRef(ref)
	minItems := 1
	supportingEpisodes := schema.Properties["supporting_episode_ids"]
	supportingEpisodes.Nullable = false
	supportingEpisodes.MinItems = &minItems
	schema.Required = append(schema.Required, "supporting_episode_ids")
}

func registerDoc[I, O any](api huma.API, id, method, path string, security []map[string][]string) {
	requestIDMaxLength := 128
	huma.Register(api, huma.Operation{
		OperationID: id,
		Method:      method,
		Path:        path,
		Summary:     strings.ToUpper(id[:1]) + strings.ReplaceAll(id[1:], "-", " "),
		Security:    security,
		Parameters:  []*huma.Param{{Name: "X-Request-ID", In: "header", Description: "Optional request ID; invalid values are replaced with a generated ID.", Schema: &huma.Schema{Type: "string", MaxLength: &requestIDMaxLength}}},
	}, func(context.Context, *I) (*O, error) { return nil, nil })
	var op *huma.Operation
	pathItem := api.OpenAPI().Paths[path]
	switch method {
	case http.MethodGet:
		op = pathItem.Get
	case http.MethodPut:
		op = pathItem.Put
	case http.MethodPost:
		op = pathItem.Post
	}
	if op.Responses == nil {
		op.Responses = make(map[string]*huma.Response)
	}
	delete(op.Responses, "default")
	errorSchema := api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[documentedErrorEnvelope](), true, "")
	for _, status := range []int{400, 401, 403, 404, 409, 413, 415, 500, 501, 503} {
		op.Responses[strconv.Itoa(status)] = &huma.Response{
			Description: http.StatusText(status),
			Content:     map[string]*huma.MediaType{"application/json": {Schema: errorSchema}},
			Headers:     requestIDResponseHeader(),
		}
	}
	op.Responses["200"].Headers = requestIDResponseHeader()
}

func addResponse(api huma.API, path, status, description string) {
	response := *api.OpenAPI().Paths[path].Post.Responses["200"]
	response.Description = description
	api.OpenAPI().Paths[path].Post.Responses[status] = &response
}

func requestIDResponseHeader() map[string]*huma.Param {
	return map[string]*huma.Param{"X-Request-ID": {Description: "Request ID returned by the server.", Schema: &huma.Schema{Type: "string"}}}
}

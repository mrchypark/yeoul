package yeoul

import (
	"fmt"
	"regexp"
	"strings"
)

// secretRule is one recognized credential shape. The rules are deliberately
// narrow, high-confidence prefixes: they are a best-effort guard for the
// well-known credential classes that carry a vendor-issued prefix, not a
// universal secret detector. Keyword rules cannot recognize arbitrary secrets,
// so callers still own the obligation not to hand Yeoul secret-bearing content.
type secretRule struct {
	name    string
	pattern *regexp.Regexp
}

var secretRules = []secretRule{
	{name: "aws_access_key_id", pattern: regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{name: "github_token", pattern: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36}\b`)},
	{name: "github_pat", pattern: regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}\b`)},
	{name: "slack_token", pattern: regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`)},
	{name: "anthropic_api_key", pattern: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`)},
	{name: "openai_api_key", pattern: regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}\b`)},
	{name: "google_api_key", pattern: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{name: "gitlab_token", pattern: regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`)},
	{name: "stripe_secret_key", pattern: regexp.MustCompile(`\b(?:sk|rk)_live_[A-Za-z0-9]{20,}\b`)},
	{name: "sendgrid_api_key", pattern: regexp.MustCompile(`\bSG\.[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}\b`)},
	{name: "digitalocean_token", pattern: regexp.MustCompile(`\bdop_v1_[a-f0-9]{64}\b`)},
	{name: "npm_token", pattern: regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`)},
	{name: "private_key_block", pattern: regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)},
	{name: "jwt", pattern: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)},
}

// secretField names one input location that is scanned before ingestion. The
// path is reported on rejection; the offending value never is.
type secretField struct {
	path  string
	value string
}

func scanSecretClass(value string) (string, bool) {
	for _, rule := range secretRules {
		if rule.pattern.MatchString(value) {
			return rule.name, true
		}
	}
	return "", false
}

// metadataSecretFields flattens a metadata map into scannable fields, including
// nested maps and slices so a secret cannot hide one level down. Keys are
// scanned too, because a credential pasted as a map key would otherwise slip
// through.
func metadataSecretFields(path string, metadata map[string]any) []secretField {
	if len(metadata) == 0 {
		return nil
	}
	fields := make([]secretField, 0, len(metadata))
	for key, value := range metadata {
		keyPath := path + "." + key
		fields = append(fields, secretField{path: keyPath, value: key})
		fields = append(fields, anySecretFields(keyPath, value)...)
	}
	return fields
}

func anySecretFields(path string, value any) []secretField {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return []secretField{{path: path, value: typed}}
	case map[string]any:
		return metadataSecretFields(path, typed)
	case []any:
		fields := make([]secretField, 0, len(typed))
		for i, item := range typed {
			fields = append(fields, anySecretFields(fmt.Sprintf("%s[%d]", path, i), item)...)
		}
		return fields
	default:
		return []secretField{{path: path, value: fmt.Sprint(typed)}}
	}
}

// rejectSecretFields fails closed on the first recognized credential. The error
// carries the input path and the rule name only, so a rejected payload is never
// echoed into diagnostics, logs, or an error response.
func rejectSecretFields(fields []secretField) error {
	for _, field := range fields {
		class, found := scanSecretClass(field.value)
		if !found {
			continue
		}
		return errorf(ErrInputInvalid, "input contains secret-like content and was rejected before ingestion", map[string]any{
			"field":        field.path,
			"secret_class": class,
			"remediation":  "remove or redact the credential before storing it; Yeoul does not persist secrets",
		}, nil)
	}
	return nil
}

func episodeSecretFields(input EpisodeInput) []secretField {
	fields := []secretField{
		{path: "episode.content", value: input.Content},
		{path: "episode.kind", value: input.Kind},
		{path: "episode.id", value: input.ID},
		{path: "episode.group_id", value: input.GroupID},
		{path: "episode.source_id", value: input.SourceID},
		{path: "episode.source.kind", value: input.Source.Kind},
		{path: "episode.source.uri", value: input.Source.URI},
		{path: "episode.source.external_ref", value: input.Source.ExternalRef},
	}
	fields = append(fields, metadataSecretFields("episode.metadata", input.Metadata)...)
	fields = append(fields, metadataSecretFields("episode.source.metadata", input.Source.Metadata)...)
	return fields
}

func entitySecretFields(input EntityInput) []secretField {
	fields := []secretField{
		{path: "entity.id", value: input.ID},
		{path: "entity.namespace", value: input.Namespace},
		{path: "entity.type", value: input.Type},
		{path: "entity.canonical_name", value: input.CanonicalName},
		{path: "entity.stable_key", value: input.StableKey},
	}
	for i, alias := range input.Aliases {
		fields = append(fields, secretField{path: fmt.Sprintf("entity.aliases[%d]", i), value: alias})
	}
	fields = append(fields, metadataSecretFields("entity.metadata", input.Metadata)...)
	return fields
}

func factSecretFields(input FactInput) []secretField {
	fields := []secretField{
		{path: "fact.id", value: input.ID},
		{path: "fact.predicate", value: input.Predicate},
		{path: "fact.subject_id", value: input.SubjectID},
		{path: "fact.object_id", value: input.ObjectID},
		{path: "fact.value_text", value: input.ValueText},
		{path: "fact.status", value: input.Status},
		{path: "fact.cardinality", value: input.Cardinality},
	}
	fields = append(fields, metadataSecretFields("fact.metadata", input.Metadata)...)
	return fields
}

// scanTextFields is a small helper for a plain string list such as supporting
// episode IDs, which can carry a pasted credential just like free text.
func textSecretFields(path string, values []string) []secretField {
	fields := make([]secretField, 0, len(values))
	for i, value := range values {
		fields = append(fields, secretField{path: fmt.Sprintf("%s[%d]", path, i), value: value})
	}
	return fields
}

func reasonSecretField(path, reason string) []secretField {
	if strings.TrimSpace(reason) == "" {
		return nil
	}
	return []secretField{{path: path, value: reason}}
}

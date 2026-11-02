package hub

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// openAPISchemaFields maps each schema of openapi.json to the Go type the
// API reads or writes for it. Clients such as the iOS app are written from
// the spec, so a JSON field that the spec leaves out is one they cannot know
// about. Add new schemas here when their Go type is a plain struct.
var openAPISchemaFields = []struct {
	schema string
	typ    any
	skip   []string // fields of the Go type that the schema leaves out on purpose
}{
	{"Monitor", store.Monitor{}, nil},
	{"MonitorInput", store.Monitor{}, []string{"id", "push_token", "paused", "status", "status_since", "created_at"}},
	{"Assertion", check.Assertion{}, nil},
	{"ChannelLink", store.ChannelLink{}, nil},
	{"Group", store.Group{}, nil},
	{"Tag", store.Tag{}, nil},
	{"Maintenance", store.Maintenance{}, nil},
	{"Notifier", store.Notifier{}, nil},
	{"StatusPage", store.StatusPage{}, nil},
	{"PageTheme", store.PageTheme{}, nil},
	{"PageColors", store.PageColors{}, nil},
	{"PageLink", store.PageLink{}, nil},
	{"PageIncident", store.PageIncident{}, nil},
	{"PageIncidentUpdate", store.PageIncidentUpdate{}, nil},
	{"Agent", store.Agent{}, nil},
	{"AuditEntry", store.AuditEntry{}, nil},
	{"GeneralSettings", GeneralSettings{}, nil},
	{"BackupSettings", BackupSettings{}, nil},
	{"ReportSettings", ReportSettings{}, nil},
}

// Every JSON field of the Go types above must be documented in the schema.
func TestOpenAPISchemasCoverFields(t *testing.T) {
	var doc struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(openAPIDoc, &doc); err != nil {
		t.Fatal(err)
	}
	schemas := doc.Components.Schemas
	for _, c := range openAPISchemaFields {
		props, ok := schemaProps(schemas, c.schema)
		if !ok {
			t.Errorf("schema %s not found", c.schema)
			continue
		}
		skip := map[string]bool{}
		for _, f := range c.skip {
			skip[f] = true
		}
		var missing []string
		for _, f := range jsonFields(reflect.TypeOf(c.typ)) {
			if !props[f] && !skip[f] {
				missing = append(missing, f)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Errorf("openapi.json schema %s misses fields of %T: %s", c.schema, c.typ, strings.Join(missing, ", "))
		}
	}
}

// schemaProps lists the properties of a schema, following allOf and $ref.
func schemaProps(schemas map[string]json.RawMessage, name string) (map[string]bool, bool) {
	raw, ok := schemas[name]
	if !ok {
		return nil, false
	}
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
		AllOf      []struct {
			Ref        string                     `json:"$ref"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"allOf"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return nil, false
	}
	out := map[string]bool{}
	for k := range s.Properties {
		out[k] = true
	}
	for _, part := range s.AllOf {
		for k := range part.Properties {
			out[k] = true
		}
		if ref := strings.TrimPrefix(part.Ref, "#/components/schemas/"); ref != "" {
			sub, ok := schemaProps(schemas, ref)
			if !ok {
				return nil, false
			}
			for k := range sub {
				out[k] = true
			}
		}
	}
	return out, true
}

// jsonFields lists the JSON names of a struct's fields, embedded structs
// included.
func jsonFields(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				out = append(out, jsonFields(ft)...)
				continue
			}
		}
		if name == "" {
			name = f.Name
		}
		out = append(out, name)
	}
	return out
}

// Copyright 2026, Jamf Software LLC

package parser

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/iancoleman/strcase"
)

// ParseMonolith derives every Jamf Pro resource from a single OpenAPI document,
// grouping paths by the collection they belong to.
//
// This replaces splitting one document into 165 per-resource files and then
// naming each resource after the file it landed in. Those filenames came from
// upstream's jss module names, appear in no spec, and silently decided four
// things: the command name, the endpoint-version family, whether a `-preview`
// tag reached a command, and whether the splitter could delete the file.
//
// Two consequences worth stating, because they are the reason this is not
// merely tidier:
//
//   - Version consolidation stops depending on a filename's `-vN` suffix. Every
//     version of a path lands in one resource and deduplicateVersionedOps picks
//     the highest per path shape, so the mis-keying that cost this CLI its v4
//     computer-inventory endpoints cannot be expressed.
//   - Each resource's schema set is the transitive $ref closure of its own
//     operations, not "whatever else was in the same file". detectNameField and
//     detectIDField scan that set, so scoping it is what keeps their answers
//     the same as before — a merged document with one shared schema map would
//     have them pick a field off an unrelated resource.
func ParseMonolith(doc *openapi3.T) ([]*Resource, error) {
	if doc == nil || doc.Paths == nil {
		return nil, fmt.Errorf("document declares no paths")
	}

	raw := map[string]*openapi3.Operation{} // method+" "+path -> operation
	tagsByPath := map[string][]string{}
	var ops []*Operation

	for _, path := range sortedKeys(doc.Paths.Map()) {
		item := doc.Paths.Map()[path]
		if item == nil {
			continue
		}
		opsMap := item.Operations()
		for _, method := range sortedKeys(opsMap) {
			op := opsMap[method]
			if op == nil {
				continue
			}
			tagsByPath[path] = append(tagsByPath[path], op.Tags...)
		}
	}

	// Dropped before anything else, so a legacy endpoint never reaches a
	// grouping decision, a name, or a command.
	for _, path := range sortedKeys(doc.Paths.Map()) {
		if !KeepPath(path, tagsByPath[path]) {
			continue
		}
		item := doc.Paths.Map()[path]
		if item == nil {
			continue
		}
		opsMap := item.Operations()
		for _, method := range sortedKeys(opsMap) {
			op := opsMap[method]
			if op == nil {
				continue
			}
			ops = append(ops, parseOperation(path, method, op))
			raw[method+" "+path] = op
		}
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("document declares no operations this generator would ingest")
	}

	opsByPath := map[string][]*Operation{}
	var paths []string
	for _, op := range ops {
		if _, seen := opsByPath[op.Path]; !seen {
			paths = append(paths, op.Path)
		}
		opsByPath[op.Path] = append(opsByPath[op.Path], op)
	}

	named := namedSchemas(doc)
	tagDescriptions := tagDescriptionsOf(doc)

	tagged := make([]TaggedPath, 0, len(paths))
	for _, p := range paths {
		tagged = append(tagged, TaggedPath{Path: p, Tag: BaseTag(firstTagOf(tagsByPath[p]))})
	}

	var resources []*Resource
	for _, group := range GroupPathsByTagAndCollection(tagged) {
		var groupOps []*Operation
		for _, p := range group.Paths {
			groupOps = append(groupOps, opsByPath[p]...)
		}
		schemas := schemasReachableFrom(doc, named, group.Paths, raw)
		representations := representationSchemas(doc, named, group.Paths)
		r := buildResourceFromGroup(group, groupOps, schemas, representations, tagDescriptions)
		if r != nil {
			resources = append(resources, r)
		}
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })
	return resources, nil
}

// buildResourceFromGroup runs the per-resource passes over one group's
// operations and assembles the Resource.
//
// The passes are per group, not global. Version deduplication, bulk-action
// pairing and name disambiguation all reason about siblings, and a document-wide
// pass would have unrelated resources competing for the same operation name —
// which is what the per-file boundary used to provide for free.
func buildResourceFromGroup(group *PathGroup, ops []*Operation, schemas, representations map[string]*Schema, tagDescriptions map[string]string) *Resource {
	nameField := detectNameField(representations)
	idField := detectIDField(schemas, ops)

	reclassifyMisannotatedCreates(ops)
	renameSingletonRootGet(ops)
	ops = deduplicateVersionedOps(ops)
	ops = pairCollectionBulkActions(ops)
	resolveNoParamConflicts(ops)
	disambiguateSameTerminalOps(ops)

	if len(ops) == 0 {
		return nil
	}

	resource := &Resource{
		Description: tagDescriptions[group.Name],
		Operations:  ops,
		Schemas:     schemas,
		NameField:   nameField,
		IDField:     idField,
	}

	// The name is the path's, verbatim — no pluralization. A collection path is
	// already plural where the API means it to be, so pluralizing it is what
	// produced `apns-client-push-statuss`, `ddm-statuss`, `csas` and `slasas`.
	resource.Name = group.Name
	resource.NameSingular = singularize(group.Name)
	resource.GoName = strcase.ToCamel(group.Name)

	if detectSingleton(ops) {
		resource.IsSingleton = true
		resource.NameSingular = group.Name
		renameSingletonListToGet(resource)
	}
	resource.HasVersionLock = detectVersionLock(ops)
	return resource
}

// renameSingletonListToGet renames a singleton's root "list" operation to
// "get", and any sub-path still carrying "list" to its terminal segment.
//
// Lifted verbatim out of ParseLoadedSpec's singleton branch. A sub-path GET
// inside a singleton is not a list endpoint either, but renaming it to "get"
// collides with the canonical root, so it takes its terminal segment instead.
func renameSingletonListToGet(resource *Resource) {
	rootPath := ""
	for _, op := range resource.Operations {
		if op.Method == "GET" && !op.IsList && !hasPathParam(op.Path) {
			if rootPath == "" || len(op.Path) < len(rootPath) {
				rootPath = op.Path
			}
		}
	}
	for _, op := range resource.Operations {
		if op.Name != "list" {
			continue
		}
		if op.Path == rootPath {
			op.Name = "get"
			continue
		}
		parts := strings.Split(op.Path, "/")
		tail := parts[len(parts)-1]
		if !strings.HasPrefix(tail, "{") && tail != "" {
			op.Name = strcase.ToKebab(tail)
		}
	}
}

// namedSchemas returns the document's component schemas by name.
func namedSchemas(doc *openapi3.T) map[string]*openapi3.SchemaRef {
	out := map[string]*openapi3.SchemaRef{}
	if doc.Components == nil {
		return out
	}
	for name, ref := range doc.Components.Schemas {
		if ref != nil && ref.Value != nil {
			out[name] = ref
		}
	}
	return out
}

// tagDescriptionsOf maps a kebab-cased tag name to the tag's description, so a
// resource can carry a spec-authored description instead of one derived from
// the document as a whole.
//
// Per-file parsing took info.description, which is a property of the file. One
// document has one of those, and a resource's own tag is the nearest thing the
// spec offers.
func tagDescriptionsOf(doc *openapi3.T) map[string]string {
	out := map[string]string{}
	for _, tag := range doc.Tags {
		if tag == nil || strings.TrimSpace(tag.Description) == "" {
			continue
		}
		out[strcase.ToKebab(tag.Name)] = strings.TrimSpace(tag.Description)
	}
	return out
}

// schemasReachableFrom returns the component schemas a group's operations
// reference, transitively.
//
// This is the closure the monolith splitter used to compute in order to decide
// which components to inline into which file. It was never really about files:
// it exists because detectNameField and detectIDField scan a resource's declared
// schemas, so handing them a document-wide map makes them answer from an
// unrelated resource's fields.
func schemasReachableFrom(doc *openapi3.T, named map[string]*openapi3.SchemaRef, paths []string, raw map[string]*openapi3.Operation) map[string]*Schema {
	return closeOver(doc, named, paths, func(*openapi3.Operation) bool { return false })
}

// representationSchemas is schemasReachableFrom restricted to the operations
// that represent the resource — everything except an `x-action: true` payload.
//
// An action's body is a command, not a representation: it says what to do to
// the resource, and its fields are the arguments. Handing those to
// detectNameField makes an argument compete to be the resource's name field,
// which is a category error that has now cost two separate defects.
//
// The first is recorded in resourceNameFieldOverrides' own comment: the mdm
// command log picked up `userName` from DeleteUserCommand and
// UnlockUserAccountCommand, and was force-cleared by hand. The second arrived
// with the merged document, where one components map means the $ref closure
// reaches the shared `ExportField{fieldName}` body of every `/export` action.
// /v1/packages gained a second typed candidate and fell back to a plain "name"
// that the endpoint refuses to filter on — 400 INVALID_FIELD, naming
// packageName among the fields it does accept — so `--name` and `apply` both
// failed outright; /v2/jamf-remote-assist/session had `fieldName` as its only
// candidate and filtered on that.
//
// Excluding the payloads is the fix rather than naming the schemas, because the
// property that disqualifies them is which operation they serve. Nothing about
// `ExportField` distinguishes it from `AccountUser`, whose `username` is the
// correct answer for /v1/accounts — both are a schema named after the field's
// own prefix — so a rule reading only the schema map cannot separate them, and
// one keyed on the resource name gets /v1/accounts wrong.
//
// detectIDField keeps the full closure: it matches a property against the get
// operation's path parameter, so a foreign schema contributes nothing unless it
// happens to carry that exact identifier, and an action's body legitimately
// does carry the resource's id.
func representationSchemas(doc *openapi3.T, named map[string]*openapi3.SchemaRef, paths []string) map[string]*Schema {
	return closeOver(doc, named, paths, isActionOperation)
}

// isActionOperation reports whether an operation is declared `x-action: true`.
func isActionOperation(op *openapi3.Operation) bool {
	action, ok := op.Extensions["x-action"]
	if !ok {
		return false
	}
	b, ok := action.(bool)
	return ok && b
}

// closeOver seeds the transitive $ref closure from every operation on paths for
// which skip is false.
func closeOver(doc *openapi3.T, named map[string]*openapi3.SchemaRef, paths []string, skip func(*openapi3.Operation) bool) map[string]*Schema {
	seeds := map[string]bool{}
	for _, p := range paths {
		item := doc.Paths.Map()[p]
		if item == nil {
			continue
		}
		for _, op := range item.Operations() {
			if op == nil || skip(op) {
				continue
			}
			collectOperationRefs(op, seeds)
		}
	}

	// Transitive closure through the named schemas themselves.
	stack := make([]string, 0, len(seeds))
	for name := range seeds {
		stack = append(stack, name)
	}
	visited := map[string]bool{}
	for len(stack) > 0 {
		name := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[name] {
			continue
		}
		visited[name] = true
		ref, ok := named[name]
		if !ok || ref.Value == nil {
			continue
		}
		nested := map[string]bool{}
		collectSchemaRefs(ref.Value, nested)
		for n := range nested {
			if !visited[n] {
				stack = append(stack, n)
			}
		}
	}

	out := make(map[string]*Schema, len(visited))
	for name := range visited {
		ref, ok := named[name]
		if !ok || ref.Value == nil {
			continue
		}
		out[name] = parseSchema(name, ref.Value)
	}
	return out
}

// collectOperationRefs records the component-schema names an operation
// references directly, through its parameters, request body and responses.
func collectOperationRefs(op *openapi3.Operation, out map[string]bool) {
	for _, p := range op.Parameters {
		if p == nil {
			continue
		}
		addRefName(p.Ref, out)
		if p.Value != nil {
			addSchemaRef(p.Value.Schema, out)
			for _, media := range p.Value.Content {
				if media != nil {
					addSchemaRef(media.Schema, out)
				}
			}
		}
	}
	if op.RequestBody != nil {
		addRefName(op.RequestBody.Ref, out)
		if op.RequestBody.Value != nil {
			for _, media := range op.RequestBody.Value.Content {
				if media != nil {
					addSchemaRef(media.Schema, out)
				}
			}
		}
	}
	if op.Responses != nil {
		for _, resp := range op.Responses.Map() {
			if resp == nil {
				continue
			}
			addRefName(resp.Ref, out)
			if resp.Value == nil {
				continue
			}
			for _, media := range resp.Value.Content {
				if media != nil {
					addSchemaRef(media.Schema, out)
				}
			}
		}
	}
}

// collectSchemaRefs records the component-schema names a schema references,
// one level deep. The caller iterates to a fixed point.
func collectSchemaRefs(s *openapi3.Schema, out map[string]bool) {
	if s == nil {
		return
	}
	for _, ref := range s.Properties {
		addSchemaRef(ref, out)
	}
	addSchemaRef(s.Items, out)
	if s.AdditionalProperties.Schema != nil {
		addSchemaRef(s.AdditionalProperties.Schema, out)
	}
	for _, set := range [][]*openapi3.SchemaRef{s.AllOf, s.AnyOf, s.OneOf} {
		for _, ref := range set {
			addSchemaRef(ref, out)
		}
	}
	if s.Not != nil {
		addSchemaRef(s.Not, out)
	}
}

// addSchemaRef records a schema reference's component name, and descends into
// an inline schema to find references nested inside it.
func addSchemaRef(ref *openapi3.SchemaRef, out map[string]bool) {
	if ref == nil {
		return
	}
	if addRefName(ref.Ref, out) {
		// A named reference stands for the whole schema; the closure loop
		// visits its contents when it resolves the name.
		return
	}
	collectSchemaRefs(ref.Value, out)
}

// addRefName extracts a component name from a "#/components/schemas/Name"
// reference and reports whether it did.
func addRefName(ref string, out map[string]bool) bool {
	const prefix = "#/components/schemas/"
	// An external reference (`Other.yaml#/components/schemas/Name`) resolves to
	// the same component in a merged document, so match on the fragment rather
	// than requiring the reference to be local.
	i := strings.Index(ref, prefix)
	if i < 0 {
		return false
	}
	name := ref[i+len(prefix):]
	if name == "" || strings.Contains(name, "/") {
		return false
	}
	out[name] = true
	return true
}

// sortedKeys returns a map's keys in sorted order, for deterministic output.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MergeDocuments unions several OpenAPI documents into one.
//
// It exists so there is a single parse path. Route A (`make sync-specs`) copies
// per-resource files out of a jamf/jss checkout and route B ingests one
// consolidated document; merging the first into the shape of the second means
// the file boundary becomes an input detail rather than something that decides
// command names.
//
// Components are unioned across every document, the shared library file
// included. A per-resource file references cross-resource definitions by
// external $ref, so its own Components block holds only part of what its
// operations reach — and a closure computed against that part would silently
// come up short, which is how detectNameField would start answering from
// nothing.
//
// A path declared by two documents is a hard error: one URL meaning two things
// is exactly what a silent overwrite hides, and upstream shipping a duplicate
// basename is an occurrence this repo already guards against elsewhere.
//
// A *component* declared by two documents is not, and must not be. The splitter
// inlines a component into every file that reaches it, so the same schema name
// legitimately appears in dozens of per-resource files — `ApiError` is in most
// of them. Identical declarations merge silently; a genuine disagreement takes
// the first and is reported, because refusing the whole ingest over a cosmetic
// difference in an error schema would block route A for nothing. Returns the
// merged document and any such reports.
func MergeDocuments(docs []*openapi3.T) (*openapi3.T, []string, error) {
	merged := &openapi3.T{
		OpenAPI:    "3.0.1",
		Info:       &openapi3.Info{Title: "Jamf Pro API", Version: "merged"},
		Paths:      openapi3.NewPaths(),
		Components: &openapi3.Components{Schemas: openapi3.Schemas{}},
	}
	pathSource := map[string]int{}
	schemaSource := map[string]int{}
	var reports []string

	for i, doc := range docs {
		if doc == nil {
			continue
		}
		if doc.Paths != nil {
			for _, p := range sortedKeys(doc.Paths.Map()) {
				if prev, dup := pathSource[p]; dup {
					return nil, nil, fmt.Errorf("path %s is declared by document %d and document %d", p, prev, i)
				}
				pathSource[p] = i
				merged.Paths.Set(p, doc.Paths.Map()[p])
			}
		}
		if doc.Components == nil {
			continue
		}
		for _, name := range sortedKeys(doc.Components.Schemas) {
			ref := doc.Components.Schemas[name]
			if ref == nil || ref.Value == nil {
				continue
			}
			if prev, dup := schemaSource[name]; dup && prev != i {
				// Compared by content, not by pointer: the splitter inlines a
				// component into every file that reaches it, so identical
				// copies under one name are the normal case and a pointer
				// comparison calls every one of them a conflict.
				if existing := merged.Components.Schemas[name]; existing != nil && !sameSchema(existing, ref) {
					reports = append(reports, fmt.Sprintf(
						"component schema %q is declared differently by document %d and document %d; keeping the first",
						name, prev, i))
				}
				continue
			}
			schemaSource[name] = i
			merged.Components.Schemas[name] = ref
		}
		if len(doc.Tags) > 0 {
			merged.Tags = append(merged.Tags, doc.Tags...)
		}
		if merged.Servers == nil && len(doc.Servers) > 0 {
			merged.Servers = doc.Servers
		}
	}
	if len(pathSource) == 0 {
		return nil, nil, fmt.Errorf("no document declared any paths")
	}
	sort.Strings(reports)
	return merged, reports, nil
}

// sameSchema reports whether two schema references declare the same thing,
// compared by their marshalled form.
//
// Content rather than identity, because the two copies being compared were
// decoded from two files and can never share a pointer even when they are
// byte-identical.
func sameSchema(a, b *openapi3.SchemaRef) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Value == b.Value {
		return true
	}
	left, errL := a.Value.MarshalJSON()
	right, errR := b.Value.MarshalJSON()
	if errL != nil || errR != nil {
		// Unmarshalable means unanswerable; treat it as a disagreement so it is
		// reported rather than assumed equal.
		return false
	}
	return string(left) == string(right)
}

// LoadDocuments loads every OpenAPI document at the given paths, merges them
// into one and derives every resource from the result.
//
// One entry point so that "how many files was the spec split into" is not a
// question the rest of the generator can ask. Documents that fail to load are
// reported and skipped rather than aborting the run, matching the per-file
// behaviour it replaces — a single malformed spec should not take the whole
// command surface with it.
func LoadDocuments(paths []string) (resources []*Resource, notes []string, err error) {
	var docs []*openapi3.T
	for _, p := range paths {
		loader := openapi3.NewLoader()
		loader.IsExternalRefsAllowed = true
		doc, loadErr := loader.LoadFromFile(p)
		if loadErr != nil {
			notes = append(notes, fmt.Sprintf("%s: %v", filepath.Base(p), loadErr))
			continue
		}
		docs = append(docs, doc)
	}
	if len(docs) == 0 {
		return nil, notes, fmt.Errorf("no document loaded from %d path(s)", len(paths))
	}

	merged, mergeNotes, err := MergeDocuments(docs)
	if err != nil {
		return nil, notes, err
	}
	notes = append(notes, mergeNotes...)

	resources, err = ParseMonolith(merged)
	if err != nil {
		return nil, notes, err
	}
	return resources, notes, nil
}

// firstTagOf returns the first tag in a path's tag list, or "" when it declares
// none. A path with no tag groups by its path root alone, which is the same
// answer the tag would have given for a tag nothing else shares.
func firstTagOf(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return tags[0]
}

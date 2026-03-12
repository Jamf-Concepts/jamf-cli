package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/iancoleman/strcase"
)

// Generator generates Go code from parsed resources
type Generator struct {
	outputDir string
}

// NewGenerator creates a new code generator
func NewGenerator(outputDir string) *Generator {
	return &Generator{outputDir: outputDir}
}

// Generate generates a Go command file for a resource
func (g *Generator) Generate(resource *Resource) (string, error) {
	// Deduplicate operations before template execution
	resource.Operations = dedupeOperations(resource.Operations)

	tmpl, err := template.New("resource").Funcs(template.FuncMap{
		"toCamel":       strcase.ToCamel,
		"toLowerCamel":  strcase.ToLowerCamel,
		"toSnake":       strcase.ToSnake,
		"toKebab":       strcase.ToKebab,
		"toScreamingSnake": strcase.ToScreamingSnake,
		"hasPathParam":  hasPathParam,
		"pathParams":    pathParams,
		"queryParams":   queryParams,
		"goType":        goType,
		"flagType":      flagType,
		"sortOps":       sortOperations,
		"dedupeOps":     dedupeOperations,
		"escapeQuotes":  escapeQuotes,
		"isDestructive": func(op *Operation) bool { return op.IsDestructive },
		"hasPostOrPut":   hasPostOrPut,
		"hasDelete":      hasDelete,
		"hasDestructive": hasDestructive,
		"needsFmt":       needsFmt,
		"defaultVal": func(paramType string, val interface{}) string {
			switch paramType {
			case "string":
				return fmt.Sprintf("%q", fmt.Sprintf("%v", val))
			case "integer":
				return fmt.Sprintf("%v", val)
			case "boolean":
				return fmt.Sprintf("%v", val)
			default:
				return fmt.Sprintf("%#v", val)
			}
		},
		"hasList": func(ops []*Operation) bool {
			for _, op := range ops {
				if op.IsList {
					return true
				}
			}
			return false
		},
	}).Parse(resourceTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	filename := fmt.Sprintf("%s.go", strings.ReplaceAll(resource.Name, "-", "_"))
	outPath := filepath.Join(g.outputDir, filename)

	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := tmpl.Execute(f, resource); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return outPath, nil
}

// GenerateRegistry generates the registry file that registers all commands
func (g *Generator) GenerateRegistry(resources []*Resource) (string, error) {
	tmpl, err := template.New("registry").Parse(registryTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	outPath := filepath.Join(g.outputDir, "registry.go")

	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Sort resources by name
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].Name < resources[j].Name
	})

	if err := tmpl.Execute(f, resources); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return outPath, nil
}

// Template helper functions
func hasPathParam(path string) bool {
	return strings.Contains(path, "{")
}

func pathParams(params []*Parameter) []*Parameter {
	var result []*Parameter
	for _, p := range params {
		if p.In == "path" {
			result = append(result, p)
		}
	}
	return result
}

func queryParams(params []*Parameter) []*Parameter {
	var result []*Parameter
	for _, p := range params {
		if p.In == "query" {
			result = append(result, p)
		}
	}
	return result
}

func goType(t string) string {
	switch t {
	case "integer":
		return "int"
	case "boolean":
		return "bool"
	case "number":
		return "float64"
	default:
		return "string"
	}
}

func flagType(t string) string {
	switch t {
	case "integer":
		return "Int"
	case "boolean":
		return "Bool"
	case "number":
		return "Float64"
	default:
		return "String"
	}
}

func escapeQuotes(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "`", "'")
	return s
}

func dedupeOperations(ops []*Operation) []*Operation {
	seen := make(map[string]bool)
	var result []*Operation
	for _, op := range ops {
		if !seen[op.Name] {
			seen[op.Name] = true
			result = append(result, op)
		}
	}
	return result
}

func hasPostOrPut(ops []*Operation) bool {
	for _, op := range ops {
		if op.Method == "POST" || op.Method == "PUT" || op.Method == "PATCH" {
			return true
		}
	}
	return false
}

func hasDelete(ops []*Operation) bool {
	for _, op := range ops {
		if op.Method == "DELETE" {
			return true
		}
	}
	return false
}

func hasDestructive(ops []*Operation) bool {
	for _, op := range ops {
		if op.IsDestructive {
			return true
		}
	}
	return false
}

func hasQueryParams(ops []*Operation) bool {
	// Only returns true for non-boolean query params (which use fmt.Sprintf)
	for _, op := range ops {
		for _, p := range op.Parameters {
			if p.In == "query" && p.Type != "boolean" {
				return true
			}
		}
	}
	return false
}

func needsFmt(ops []*Operation) bool {
	// fmt is needed for: destructive confirmations, query param formatting, delete success message
	return hasDestructive(ops) || hasQueryParams(ops) || hasDelete(ops)
}

func sortOperations(ops []*Operation) []*Operation {
	// Define order: list, get, create, update, delete, then others
	order := map[string]int{
		"list":            1,
		"get":             2,
		"create":          3,
		"update":          4,
		"delete":          5,
		"delete-multiple": 6,
		"history":         7,
		"add-history-note": 8,
		"export":          9,
		"history-export":  10,
	}

	sorted := make([]*Operation, len(ops))
	copy(sorted, ops)

	sort.Slice(sorted, func(i, j int) bool {
		oi, oki := order[sorted[i].Name]
		oj, okj := order[sorted[j].Name]
		if !oki {
			oi = 100
		}
		if !okj {
			oj = 100
		}
		return oi < oj
	})

	return sorted
}

const resourceTemplate = `// Code generated by jamfpro-cli generator. DO NOT EDIT.
package generated

import (
	"context"
{{- if hasList .Operations }}
	"encoding/json"
{{- end }}
{{- if or (needsFmt .Operations) (hasList .Operations) }}
	"fmt"
{{- end }}
{{- if or (hasPostOrPut .Operations) (hasList .Operations) }}
	"io"
{{- end }}
{{- if hasDelete .Operations }}
	"net/http"
{{- end }}
{{- if or (hasPostOrPut .Operations) (hasDestructive .Operations) (hasDelete .Operations) }}
	"os"
{{- end }}
	"strings"

	"github.com/spf13/cobra"
)

// New{{ .GoName }}Cmd creates the {{ .Name }} command group
func New{{ .GoName }}Cmd(ctx *CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "{{ .Name }}",
		Short: "Manage {{ .Name }}",
		Long:  ` + "`" + `Manage {{ .Name }} in Jamf Pro.` + "`" + `,
	}
{{ range dedupeOps (sortOps .Operations) }}
	cmd.AddCommand(new{{ $.GoName }}{{ toCamel .Name }}Cmd(ctx))
{{- end }}

	return cmd
}
{{ range dedupeOps (sortOps .Operations) }}
func new{{ $.GoName }}{{ toCamel .Name }}Cmd(ctx *CLIContext) *cobra.Command {
	var (
{{- range queryParams .Parameters }}
{{- if .IsArray }}
		flag{{ toCamel .Name }} []{{ goType .Type }}
{{- else }}
		flag{{ toCamel .Name }} {{ goType .Type }}
{{- end }}
{{- end }}
{{- if .IsList }}
		flagAll  bool
		flagLimit int
{{- end }}
{{- if .IsDestructive }}
		flagYes bool
		flagDryRun bool
{{- end }}
{{- if eq .Name "delete-multiple" }}
		flagIds []string
{{- end }}
	)

	cmd := &cobra.Command{
		Use:   "{{ .Name }}{{ if hasPathParam .Path }} <id>{{ end }}",
		Short: "{{ escapeQuotes .Summary }}",
{{- if .Description }}
		Long:  "{{ escapeQuotes .Description }}",
{{- end }}
{{- if hasPathParam .Path }}
		Args:  cobra.ExactArgs(1),
{{- end }}
		RunE: func(cmd *cobra.Command, args []string) error {
			reqCtx := context.Background()
{{- if .IsDestructive }}

			// Confirmation for destructive action
			if flagDryRun {
				fmt.Fprintf(os.Stderr, "Would {{ .Name }}{{ if hasPathParam .Path }} resource %s{{ end }}\n"{{ if hasPathParam .Path }}, args[0]{{ end }})
				return nil
			}
			if !flagYes {
				noInput, _ := cmd.Flags().GetBool("no-input")
				if noInput {
					return fmt.Errorf("destructive operation requires --yes when --no-input is set")
				}
				fmt.Fprintf(os.Stderr, "⚠️  This will {{ .Name }}{{ if hasPathParam .Path }} resource %s{{ end }}. Type 'yes' to confirm: "{{ if hasPathParam .Path }}, args[0]{{ end }})
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != "yes" {
					return fmt.Errorf("aborted")
				}
			}
{{- end }}

			// Build request path
			path := "{{ .Path }}"
{{- range pathParams .Parameters }}
			path = strings.Replace(path, "{{"{"}}{{ .Name }}{{"}"}}", args[0], 1)
{{- end }}

			// Build query string
			var queryParts []string
{{- range queryParams .Parameters }}
{{- if .IsArray }}
			if len(flag{{ toCamel .Name }}) > 0 {
				for _, v := range flag{{ toCamel .Name }} {
					queryParts = append(queryParts, fmt.Sprintf("{{ .Name }}=%v", v))
				}
			}
{{- else if eq .Type "string" }}
			if flag{{ toCamel .Name }} != "" {
				queryParts = append(queryParts, fmt.Sprintf("{{ .Name }}=%s", flag{{ toCamel .Name }}))
			}
{{- else if eq .Type "integer" }}
			if flag{{ toCamel .Name }} != 0 {
				queryParts = append(queryParts, fmt.Sprintf("{{ .Name }}=%d", flag{{ toCamel .Name }}))
			}
{{- else if eq .Type "boolean" }}
			if flag{{ toCamel .Name }} {
				queryParts = append(queryParts, "{{ .Name }}=true")
			}
{{- end }}
{{- end }}
			if len(queryParts) > 0 {
				path = path + "?" + strings.Join(queryParts, "&")
			}
{{- if .IsList }}

			// Auto-pagination: fetch all pages when --all is set and --page was not manually specified
			if flagAll && flagPage == 0 {
				var allResults []json.RawMessage
				pageNum := 0
				pageSize := 100

				for {
					// Build page-specific query
					pagePath := "{{ .Path }}"
{{- range pathParams .Parameters }}
					pagePath = strings.Replace(pagePath, "{{"{"}}{{ .Name }}{{"}"}}", args[0], 1)
{{- end }}
					var pageQuery []string
					// Carry forward non-pagination query params
					for _, qp := range queryParts {
						if !strings.HasPrefix(qp, "page=") && !strings.HasPrefix(qp, "page-size=") && !strings.HasPrefix(qp, "pagesize=") {
							pageQuery = append(pageQuery, qp)
						}
					}
					pageQuery = append(pageQuery, fmt.Sprintf("page=%d", pageNum))
					pageQuery = append(pageQuery, fmt.Sprintf("page-size=%d", pageSize))
					pagePath = pagePath + "?" + strings.Join(pageQuery, "&")

					resp, err := ctx.Client.Do(reqCtx, "GET", pagePath, nil)
					if err != nil {
						return err
					}

					body, err := io.ReadAll(resp.Body)
					resp.Body.Close()
					if err != nil {
						return err
					}

					// Parse pagination response: {"totalCount": N, "results": [...]}
					var pageResp struct {
						TotalCount int               ` + "`" + `json:"totalCount"` + "`" + `
						Results    []json.RawMessage  ` + "`" + `json:"results"` + "`" + `
					}
					if err := json.Unmarshal(body, &pageResp); err != nil {
						// Not a paginated response; output as-is
						return ctx.Output.PrintRaw(body)
					}

					allResults = append(allResults, pageResp.Results...)

					// Check limit
					if flagLimit > 0 && len(allResults) >= flagLimit {
						allResults = allResults[:flagLimit]
						break
					}

					// Check if we've fetched everything
					if len(pageResp.Results) < pageSize || len(allResults) >= pageResp.TotalCount {
						break
					}

					pageNum++
				}

				// Output combined results as JSON array
				combined, err := json.MarshalIndent(allResults, "", "  ")
				if err != nil {
					return err
				}
				return ctx.Output.PrintRaw(combined)
			}
{{- end }}

			// Make request
{{- if eq .Method "GET" "DELETE" }}
			resp, err := ctx.Client.Do(reqCtx, "{{ .Method }}", path, nil)
{{- else }}
			// Read body from stdin if available
			var body io.Reader
{{- if eq .Name "delete-multiple" }}
			// Handle --ids flag for bulk operations
			if len(flagIds) > 0 {
				idsJSON := fmt.Sprintf(` + "`" + `{"ids":[%s]}` + "`" + `, strings.Join(func() []string {
					quoted := make([]string, len(flagIds))
					for i, id := range flagIds {
						quoted[i] = fmt.Sprintf(` + "`" + `"%s"` + "`" + `, id)
					}
					return quoted
				}(), ","))
				body = strings.NewReader(idsJSON)
			} else {
				stat, _ := os.Stdin.Stat()
				if (stat.Mode() & os.ModeCharDevice) == 0 {
					body = os.Stdin
				}
			}
{{- else }}
			stat, _ := os.Stdin.Stat()
			if (stat.Mode() & os.ModeCharDevice) == 0 {
				body = os.Stdin
			}
{{- end }}
			resp, err := ctx.Client.Do(reqCtx, "{{ .Method }}", path, body)
{{- end }}
			if err != nil {
				return err
			}
			defer resp.Body.Close()

{{ if eq .Method "DELETE" }}
			if resp.StatusCode == http.StatusNoContent {
				fmt.Fprintln(os.Stderr, "Deleted successfully")
				return nil
			}
{{ end }}
			return ctx.Output.PrintResponse(resp)
		},
	}
{{ range queryParams .Parameters }}
{{- if .IsArray }}
	cmd.Flags().{{ flagType .Type }}SliceVar(&flag{{ toCamel .Name }}, "{{ toKebab .Name }}", nil, "{{ escapeQuotes .Description }}")
{{- else }}
	cmd.Flags().{{ flagType .Type }}Var(&flag{{ toCamel .Name }}, "{{ toKebab .Name }}", {{ if .Default }}{{ defaultVal .Type .Default }}{{ else if eq .Type "string" }}""{{ else if eq .Type "integer" }}0{{ else if eq .Type "boolean" }}false{{ else }}0{{ end }}, "{{ escapeQuotes .Description }}")
{{- end }}
{{- end }}
{{- if .IsList }}
	cmd.Flags().BoolVar(&flagAll, "all", true, "Fetch all pages (set --all=false for single page)")
	cmd.Flags().IntVar(&flagLimit, "limit", 0, "Maximum total results to return (0 = unlimited)")
{{- end }}
{{- if .IsDestructive }}
	cmd.Flags().BoolVar(&flagYes, "yes", false, "Skip confirmation prompt")
	cmd.Flags().BoolVarP(&flagDryRun, "dry-run", "n", false, "Preview without executing")
{{- end }}
{{- if eq .Name "delete-multiple" }}
	cmd.Flags().StringSliceVar(&flagIds, "ids", nil, "IDs to delete (comma-separated)")
{{- end }}

	return cmd
}
{{ end }}
`

const registryTemplate = `// Code generated by jamfpro-cli generator. DO NOT EDIT.
package generated

import (
	"context"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

// HTTPClient interface for making API requests
type HTTPClient interface {
	Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error)
}

// OutputFormatter interface for formatting output
type OutputFormatter interface {
	PrintResponse(resp *http.Response) error
	PrintRaw(data []byte) error
}

// CLIContext holds the shared client and output formatter for all commands.
// It is populated in PersistentPreRunE after token/URL resolution.
type CLIContext struct {
	Client HTTPClient
	Output OutputFormatter
}

// RegisterCommands registers all generated resource commands
func RegisterCommands(root *cobra.Command, ctx *CLIContext) {
{{- range . }}
	root.AddCommand(New{{ .GoName }}Cmd(ctx))
{{- end }}
}
`

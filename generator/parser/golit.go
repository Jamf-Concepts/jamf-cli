// Copyright 2026, Jamf Software LLC

package parser

import (
	"bytes"
	"fmt"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"unicode"
)

// commandNamePattern is the shape of every committed command and resource
// name; a name outside it reaches an identifier or a filename, so it is refused.
var commandNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// ValidateCommandName refuses a spec-derived name that is not lowercase kebab-case.
func ValidateCommandName(kind, name string) error {
	if !commandNamePattern.MatchString(name) {
		return fmt.Errorf("%s %q is not a lowercase kebab-case name; refusing to emit it into generated Go", kind, name)
	}
	return nil
}

// ValidateResourceNames refuses a resource whose name or any operation name,
// including one set by x-operation-name, is not lowercase kebab-case.
func ValidateResourceNames(r *Resource) error {
	if err := ValidateCommandName("resource name", r.Name); err != nil {
		return err
	}
	if r.Parent != "" {
		if err := ValidateCommandName("parent resource name", r.Parent); err != nil {
			return err
		}
	}
	for _, op := range r.Operations {
		if err := ValidateCommandName(r.Name+" operation name", op.Name); err != nil {
			return err
		}
		if err := validatePath(r.Name, op.Path); err != nil {
			return err
		}
		if err := validatePath(r.Name, op.BulkActionPath); err != nil {
			return err
		}
		if err := validateDisplayText(r.Name, op); err != nil {
			return err
		}
	}
	if r.UpdateTokenOp != nil {
		if err := validatePath(r.Name, r.UpdateTokenOp.Path); err != nil {
			return err
		}
	}
	return nil
}

// validatePath refuses a control character in a spec path, which the
// templates also place in `//` comments that a newline would end.
func validatePath(resource, path string) error {
	if i := strings.IndexFunc(path, unicode.IsControl); i >= 0 {
		return fmt.Errorf("%s path %q contains a control character; refusing to emit it into generated Go", resource, path)
	}
	return nil
}

// validateDisplayText refuses a control character in an operation's summary,
// description, parameter descriptions or required privileges. strconv.Quote
// keeps such a byte out of the Go lexer but compiles it back to a live byte,
// which --help and the 403 hint would then write to a terminal. A line break
// and a tab are kept in prose, where a spec uses them for layout; a privilege
// name has no use for either.
func validateDisplayText(resource string, op *Operation) error {
	prose := func(r rune) bool { return r != '\n' && r != '\t' && unicode.IsControl(r) }
	check := func(what, s string, bad func(rune) bool) error {
		if strings.IndexFunc(s, bad) >= 0 {
			return fmt.Errorf("%s %s %s %q contains a control character; refusing to emit it into generated Go", resource, op.Name, what, s)
		}
		return nil
	}
	if err := check("summary", op.Summary, prose); err != nil {
		return err
	}
	if err := check("description", op.Description, prose); err != nil {
		return err
	}
	for _, p := range op.Parameters {
		if err := check("parameter "+p.Name+" description", p.Description, prose); err != nil {
			return err
		}
	}
	for _, priv := range op.Privileges {
		if err := check("required privilege", priv, unicode.IsControl); err != nil {
			return err
		}
	}
	return nil
}

// GoStructTag renders `json:"key"` as a Go literal that no key can close.
func GoStructTag(key string) string {
	tag := "json:" + strconv.Quote(key)
	if strconv.CanBackquote(tag) {
		return "`" + tag + "`"
	}
	return strconv.Quote(tag)
}

// WriteGoSource renders tmpl and refuses output that is not valid Go; nothing
// is written on failure, and formatting is left to `make generate`'s go fmt.
func WriteGoSource(outPath string, tmpl *template.Template, data any) error {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), outPath, buf.Bytes(), goparser.SkipObjectResolution); err != nil {
		return fmt.Errorf("generated %s is not valid Go: %w", filepath.Base(outPath), err)
	}
	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("creating file %s: %w", outPath, err)
	}
	return nil
}

// GoRawString renders s as a raw string literal when it can hold s unchanged,
// and as an interpreted literal otherwise.
func GoRawString(s string) string {
	if !strings.ContainsAny(s, "`\r") {
		return "`" + s + "`"
	}
	return strconv.Quote(s)
}

// GoLiteralFuncs are the template funcs every emitter uses to place a
// spec-derived string in generated Go.
func GoLiteralFuncs() template.FuncMap {
	return template.FuncMap{
		"goStr":       strconv.Quote,
		"goRaw":       GoRawString,
		"goStructTag": GoStructTag,
	}
}

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

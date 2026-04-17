// Copyright 2026, Jamf Software LLC

package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"maps"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Jamf-Concepts/jamf-cli/internal/xmlconv"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorDim    = "\033[2m"
	colorBold   = "\033[1m"
)

// Status symbols
const (
	symbolActive   = "●"
	symbolInactive = "○"
	symbolPending  = "◐"
)

// Format represents an output format
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatCSV   Format = "csv"
	FormatYAML  Format = "yaml"
	FormatPlain Format = "plain"
	FormatXML   Format = "xml" // Classic API native format — pretty-printed XML
	FormatRaw   Format = "raw" // Exact wire bytes, no conversion or formatting
)

// nowFunc is the function used to get the current time. Override in tests.
var nowFunc = time.Now

// Formatter handles output formatting
type Formatter struct {
	format  Format
	writer  io.Writer
	noColor bool
	wide    bool
}

// SetWriter replaces the output destination.
func (f *Formatter) SetWriter(w io.Writer) {
	f.writer = w
}

// Format returns the current output format string.
func (f *Formatter) Format() string {
	return string(f.format)
}

// PrintBytes writes bytes to the output.
// XML is pretty-printed unless the format is FormatRaw, which writes exact wire bytes.
// Used by Classic API commands to emit XML when no structured format is requested.
func (f *Formatter) PrintBytes(data []byte) error {
	if f.format != FormatRaw && xmlconv.IsXML(data) {
		if pretty, err := prettyXML(data); err == nil {
			data = pretty
		}
	}
	_, err := f.writer.Write(data)
	if err != nil {
		return err
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		_, err = f.writer.Write([]byte("\n"))
	}
	return err
}

// prettyXML re-serializes XML with 2-space indentation.
// Returns an error and leaves the caller to use the original data if parsing fails.
func prettyXML(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	dec := xml.NewDecoder(bytes.NewReader(data))
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := enc.EncodeToken(tok); err != nil {
			return nil, err
		}
	}
	if err := enc.Flush(); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// New creates a new formatter
func New(format string, noColor bool, wide bool) *Formatter {
	return &Formatter{
		format:  Format(format),
		writer:  os.Stdout,
		noColor: noColor,
		wide:    wide,
	}
}

// Print outputs data in the configured format
func (f *Formatter) Print(data any) error {
	switch f.format {
	case FormatJSON:
		return f.printJSON(data)
	case FormatYAML:
		return f.printYAML(data)
	case FormatCSV:
		return f.printCSV(data)
	case FormatPlain:
		return f.printPlain(data)
	default:
		return f.printTable(data)
	}
}

func (f *Formatter) printJSON(data any) error {
	enc := json.NewEncoder(f.writer)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func (f *Formatter) printYAML(data any) error {
	enc := yaml.NewEncoder(f.writer)
	enc.SetIndent(2)
	return enc.Encode(data)
}

func (f *Formatter) printCSV(data any) error {
	w := csv.NewWriter(f.writer)

	switch v := data.(type) {
	case []map[string]any:
		if len(v) == 0 {
			return nil
		}
		v = flattenRows(v)
		headers := sortedKeys(v[0])
		_ = w.Write(headers)
		for _, row := range v {
			vals := make([]string, len(headers))
			for i, h := range headers {
				vals[i] = FormatValue(row[h])
			}
			_ = w.Write(vals)
		}
	default:
		return fmt.Errorf("CSV output not supported for type %T", data)
	}
	w.Flush()
	return w.Error()
}

func (f *Formatter) printPlain(data any) error {
	// Tab-separated, no headers
	switch v := data.(type) {
	case []map[string]any:
		v = flattenRows(v)
		for _, row := range v {
			keys := sortedKeys(row)
			vals := make([]string, len(keys))
			for i, k := range keys {
				vals[i] = FormatValue(row[k])
			}
			_, _ = fmt.Fprintln(f.writer, strings.Join(vals, "\t"))
		}
	case string:
		_, _ = fmt.Fprintln(f.writer, v)
	default:
		_, _ = fmt.Fprintf(f.writer, "%v\n", data)
	}
	return nil
}

func (f *Formatter) printTable(data any) error {
	rows, ok := data.([]map[string]any)
	if !ok {
		_, _ = fmt.Fprintf(f.writer, "%v\n", data)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}

	// Flatten nested objects to dot-notation columns for readable table output
	rows = flattenRows(rows)

	allKeys := sortedKeys(rows[0])

	// Filter columns unless --wide is set
	var keys []string
	if f.wide {
		keys = allKeys
	} else {
		keys = defaultColumns(allKeys, rows[0])
		if len(keys) == 0 {
			keys = allKeys // fallback if no default columns found
		}
	}

	// Calculate column widths (using formatted values)
	widths := make([]int, len(keys))
	for i, k := range keys {
		widths[i] = len(strings.ToUpper(k))
	}
	for _, row := range rows {
		for i, k := range keys {
			val := FormatValue(row[k])
			// Apply date formatting for width calculation
			if isDateColumn(k) {
				val, _ = formatDateValue(val, f.wide)
			}
			if len(val) > widths[i] {
				widths[i] = len(val)
			}
		}
	}

	// Summary header
	_, _ = fmt.Fprintf(f.writer, "%s (%d total)\n\n",
		f.colorize("RESULTS", colorBold), len(rows))

	// Header row
	var headerParts []string
	for i, k := range keys {
		headerParts = append(headerParts, fmt.Sprintf("%-*s", widths[i], strings.ToUpper(k)))
	}
	_, _ = fmt.Fprintf(f.writer, " %s\n", strings.Join(headerParts, "   "))

	// Separator line
	totalWidth := 1
	for _, w := range widths {
		totalWidth += w + 3
	}
	_, _ = fmt.Fprintln(f.writer, strings.Repeat("─", totalWidth))

	// Data rows
	for _, row := range rows {
		var valParts []string
		for i, k := range keys {
			val := FormatValue(row[k])
			displayVal := val

			// Apply date formatting and colorization
			if isDateColumn(k) {
				formattedDate, isRecent := formatDateValue(val, f.wide)
				val = formattedDate
				if isRecent {
					displayVal = f.colorize(formattedDate, colorGreen)
				} else {
					displayVal = formattedDate
				}
			} else if isStatusColumn(k) {
				// Apply status colorization
				displayVal = f.formatStatusValue(val)
			}

			// Pad based on raw value width (ANSI codes are invisible)
			padding := widths[i] - len(val) + len(displayVal)
			valParts = append(valParts, fmt.Sprintf("%-*s", padding, displayVal))
		}
		_, _ = fmt.Fprintf(f.writer, " %s\n", strings.Join(valParts, "   "))
	}

	return nil
}

// PrintRaw outputs raw bytes (usually JSON from the API).
// XML responses (from Classic API) are converted to JSON before formatting,
// unless the format is FormatXML (pretty-printed) or FormatRaw (exact wire bytes).
func (f *Formatter) PrintRaw(data []byte) error {
	// FormatRaw: exact wire bytes, no processing at all.
	if f.format == FormatRaw {
		_, err := f.writer.Write(data)
		return err
	}
	// FormatXML: pretty-print and pass through without JSON conversion.
	if f.format == FormatXML {
		return f.PrintBytes(data)
	}

	// Convert XML to JSON if needed (Classic API responses).
	if xmlconv.IsXML(data) {
		if converted, err := xmlconv.ToJSON(data); err == nil {
			data = converted
		}
	}

	if f.format == FormatJSON {
		// Pretty-print JSON so compact API responses become readable
		var buf bytes.Buffer
		if err := json.Indent(&buf, data, "", "  "); err != nil {
			// Not valid JSON, print as-is
			_, writeErr := f.writer.Write(data)
			return writeErr
		}
		buf.WriteByte('\n')
		_, err := f.writer.Write(buf.Bytes())
		return err
	}

	// For non-JSON formats: parse, normalize, then format
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		// Not JSON, print as-is
		_, writeErr := f.writer.Write(data)
		return writeErr
	}

	return f.Print(normalizeJSON(parsed))
}

// normalizeJSON converts parsed JSON types into the []map[string]interface{}
// form that table/csv/plain formatters expect.
func normalizeJSON(data any) any {
	switch v := data.(type) {
	case []any:
		// Convert []interface{} of objects to []map[string]interface{}
		maps := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				maps = append(maps, m)
			} else {
				// Mixed array — can't normalize to maps, return as-is
				return data
			}
		}
		return maps
	case map[string]any:
		// Wrap single object in a slice
		return []map[string]any{v}
	default:
		// Scalar values pass through unchanged
		return data
	}
}

// PrintError outputs an error in the appropriate format
func (f *Formatter) PrintError(err error, code string, details map[string]any) {
	if f.format == FormatJSON {
		errObj := map[string]any{
			"error":   code,
			"message": err.Error(),
		}
		maps.Copy(errObj, details)
		_ = f.printJSON(errObj)
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
	}
}

// keyPriority returns the sort priority for a column key.
// "id" gets priority 0, "name" (or "section.name") gets priority 1, everything else gets 2.
func keyPriority(key string) int {
	switch key {
	case "id":
		return 0
	case "name":
		return 1
	}
	// Dotted keys: "general.name" → priority 1 (single nesting level only)
	parts := strings.Split(key, ".")
	if len(parts) == 2 && parts[1] == "name" {
		return 1
	}
	return 2
}

// sortedKeys returns map keys in deterministic order:
// "id" first, then "name", then remaining keys alphabetically.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		pi, pj := keyPriority(keys[i]), keyPriority(keys[j])
		if pi != pj {
			return pi < pj
		}
		return keys[i] < keys[j]
	})
	return keys
}

// defaultColumnLimit is the maximum number of columns shown without --wide.
// id and name are always first (via sortedKeys), then remaining columns
// in alphabetical order up to the limit.
const defaultColumnLimit = 8

// defaultColumns returns columns to show when not in wide mode.
// Shows up to defaultColumnLimit columns in sortedKeys order (id first,
// name second, then alphabetical). Array-valued columns (which render as
// huge compact JSON in table mode) are pushed after scalar columns so they
// only fill remaining slots. Use --wide for all columns.
func defaultColumns(allKeys []string, firstRow map[string]any) []string {
	if len(allKeys) <= defaultColumnLimit {
		return allKeys
	}
	var scalars, arrays []string
	for _, k := range allKeys {
		if _, isArr := firstRow[k].([]any); isArr {
			arrays = append(arrays, k)
		} else {
			scalars = append(scalars, k)
		}
	}
	result := scalars
	if len(result) < defaultColumnLimit {
		remaining := min(defaultColumnLimit-len(result), len(arrays))
		result = append(result, arrays[:remaining]...)
	}
	if len(result) > defaultColumnLimit {
		result = result[:defaultColumnLimit]
	}
	return result
}

// flattenRows flattens nested objects in each row to dot-notation keys for
// table/csv/plain display. Empty objects, empty arrays, and nil values are
// dropped. Non-empty arrays are kept as-is. Returns the original slice
// unchanged when no row contains a nested object (zero allocation fast path).
func flattenRows(rows []map[string]any) []map[string]any {
	needsFlatten := false
	for _, row := range rows {
		for _, v := range row {
			if m, ok := v.(map[string]any); ok && len(m) > 0 {
				needsFlatten = true
				break
			}
		}
		if needsFlatten {
			break
		}
	}
	if !needsFlatten {
		return rows
	}
	result := make([]map[string]any, len(rows))
	for i, row := range rows {
		flat := make(map[string]any)
		flattenMap(flat, "", row)
		result[i] = flat
	}
	return stripCommonPrefix(result)
}

// flattenMap recursively flattens nested maps into dot-notation keys.
func flattenMap(dst map[string]any, prefix string, src map[string]any) {
	for k, v := range src {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]any:
			if len(val) == 0 {
				continue
			}
			flattenMap(dst, key, val)
		case []any:
			if len(val) == 0 {
				continue
			}
			dst[key] = v
		case nil:
			continue
		default:
			dst[key] = v
		}
	}
}

// stripCommonPrefix removes a shared dot-notation prefix from flattened keys
// when all dotted keys share the same first segment (e.g. all start with
// "general."). This turns "general.name" → "name", "general.platform" →
// "platform", etc. If keys come from multiple sections or stripping would
// collide with an existing key, the rows are returned unchanged.
func stripCommonPrefix(rows []map[string]any) []map[string]any {
	if len(rows) == 0 {
		return rows
	}
	first := rows[0]

	// Collect dotted keys and non-dotted keys
	var dottedKeys []string
	nonDotted := make(map[string]bool)
	for k := range first {
		if strings.Contains(k, ".") {
			dottedKeys = append(dottedKeys, k)
		} else {
			nonDotted[k] = true
		}
	}
	if len(dottedKeys) < 2 {
		return rows
	}

	// Find common first-segment prefix
	dot := strings.Index(dottedKeys[0], ".")
	if dot < 0 {
		return rows
	}
	prefix := dottedKeys[0][:dot+1]
	for _, k := range dottedKeys[1:] {
		if !strings.HasPrefix(k, prefix) {
			return rows
		}
	}

	// Check for collisions with existing non-dotted keys
	for _, k := range dottedKeys {
		if nonDotted[strings.TrimPrefix(k, prefix)] {
			return rows
		}
	}

	// Strip the prefix from all rows
	result := make([]map[string]any, len(rows))
	for i, row := range rows {
		newRow := make(map[string]any, len(row))
		for k, v := range row {
			newRow[strings.TrimPrefix(k, prefix)] = v
		}
		result[i] = newRow
	}
	return result
}

// FormatValue converts a value to its string representation for table/csv/plain output.
func FormatValue(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case float64:
		// Display integers without decimal point
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case map[string]any, []any:
		// Nested objects/arrays become compact JSON
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// colorize wraps text with ANSI color codes, respecting noColor flag
func (f *Formatter) colorize(text, colorCode string) string {
	if f.noColor {
		return text
	}
	return colorCode + text + colorReset
}

// isStatusColumn returns true if column name suggests status values.
// Uses suffix matching to avoid false positives (e.g., "stateProvince" won't
// match because it doesn't end with "state").
func isStatusColumn(name string) bool {
	lower := strings.ToLower(name)

	// Date columns are not status columns (e.g., lastEnrolledDate contains "enrolled")
	if isDateColumn(name) {
		return false
	}

	suffixes := []string{
		"status", "state", "health", "managed", "enrolled",
		"supervised", "active", "enabled", "connected", "mdm", "approved", "remote",
	}
	for _, s := range suffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	return false
}

// isDateColumn returns true if column name suggests date/time values
func isDateColumn(name string) bool {
	lower := strings.ToLower(name)
	patterns := []string{"date", "time", "timestamp", "created", "updated", "modified"}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// absoluteDate formats a time as a human-readable absolute date string.
func absoluteDate(t time.Time) string {
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 {
		return t.Format("Jan 02, 2006")
	}
	return t.Format("Jan 02, 2006 3:04 PM")
}

// parseDate attempts to parse a string as a date using common ISO 8601 formats.
// Returns the parsed time and true on success, or zero time and false on failure.
func parseDate(value string) (time.Time, bool) {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// relativeDate returns a short relative time string for recent dates.
// For dates older than 30 days or in the future, it returns the absolute format.
func relativeDate(t time.Time) string {
	d := nowFunc().Sub(t)
	if d < 0 {
		return absoluteDate(t)
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	default:
		return absoluteDate(t)
	}
}

// formatDateValue converts ISO 8601 dates to human-readable format.
// In wide mode, always returns absolute dates. Otherwise returns relative
// times for recent dates. The second return value indicates whether the
// date is recent (within 24 hours).
func formatDateValue(value string, wide bool) (string, bool) {
	if value == "" {
		return "", false
	}

	t, ok := parseDate(value)
	if !ok {
		return value, false
	}

	d := nowFunc().Sub(t)
	isRecent := d >= 0 && d < 24*time.Hour

	if wide {
		return absoluteDate(t), isRecent
	}

	return relativeDate(t), isRecent
}

// formatStatusValue applies color and symbol to status-like values
func (f *Formatter) formatStatusValue(value string) string {
	lower := strings.ToLower(value)

	// Check more specific patterns first to avoid substring matches
	// e.g., "inactive" contains "active", so check inactive first

	// Inactive states (dim) - check BEFORE active
	for _, p := range []string{
		"inactive", "disabled", "stale", "false",
		"unmanaged", "unenrolled", "disconnected", "offline", "no",
	} {
		if strings.Contains(lower, p) {
			return f.colorize(symbolInactive+" "+value, colorDim)
		}
	}

	// Error states (red) - check before active since "unhealthy" contains "healthy"
	for _, p := range []string{"error", "failed", "critical", "unhealthy"} {
		if strings.Contains(lower, p) {
			return f.colorize(symbolActive+" "+value, colorRed)
		}
	}

	// Active states (green)
	for _, p := range []string{
		"active", "enabled", "healthy", "true", "managed",
		"enrolled", "supervised", "connected", "online", "yes",
	} {
		if strings.Contains(lower, p) {
			return f.colorize(symbolActive+" "+value, colorGreen)
		}
	}

	// Pending states (yellow)
	for _, p := range []string{"pending", "unknown", "warning"} {
		if strings.Contains(lower, p) {
			return f.colorize(symbolPending+" "+value, colorYellow)
		}
	}

	return value
}

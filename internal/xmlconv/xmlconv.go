package xmlconv

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// IsXML returns true if data appears to be XML (starts with '<' after whitespace).
func IsXML(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	return len(trimmed) > 0 && trimmed[0] == '<'
}

// ToJSON converts XML bytes to JSON bytes. The root XML element becomes the
// top-level JSON key: <policy>...</policy> → {"policy": {...}}.
//
// Collection elements that contain a <size> child are treated as arrays:
// their non-<size> children are collected into a JSON array. This matches
// the Jamf Classic API convention where list containers always include <size>.
func ToJSON(xmlData []byte) ([]byte, error) {
	m, err := ToMap(xmlData)
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// ToMap converts XML bytes to a map. The root element name is the top-level key.
// See ToJSON for array-detection heuristics.
func ToMap(xmlData []byte) (map[string]any, error) {
	root, err := parseXML(xmlData)
	if err != nil {
		return nil, err
	}
	return map[string]any{root.name: nodeToValue(root)}, nil
}

// CountListItems counts the direct child elements of the XML root, excluding
// any <size> element. This is purpose-built for Jamf Classic API list endpoints
// like /JSSResource/policies which return:
//
//	<policies><size>2</size><policy>...</policy><policy>...</policy></policies>
func CountListItems(xmlData []byte) (int, error) {
	root, err := parseXML(xmlData)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, child := range root.children {
		if child.name == "size" {
			continue
		}
		count++
	}
	return count, nil
}

// ExtractListItems extracts the repeated child elements from a Classic API list
// XML response as a slice of maps, skipping <size>. Each child element is
// recursively converted to a map.
func ExtractListItems(xmlData []byte) ([]map[string]any, error) {
	root, err := parseXML(xmlData)
	if err != nil {
		return nil, err
	}
	var items []map[string]any
	for _, child := range root.children {
		if child.name == "size" {
			continue
		}
		val := nodeToValue(child)
		if m, ok := val.(map[string]any); ok {
			items = append(items, m)
		}
	}
	if items == nil {
		items = []map[string]any{}
	}
	return items, nil
}

// xmlNode represents a parsed XML element.
type xmlNode struct {
	name     string
	text     string
	children []*xmlNode
}

// parseXML tokenizes XML data into a tree of xmlNode values.
func parseXML(data []byte) (*xmlNode, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var root *xmlNode
	var stack []*xmlNode

	for {
		tok, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("parsing XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			node := &xmlNode{name: t.Name.Local}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			} else {
				root = node
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				text := strings.TrimSpace(string(t))
				if text != "" {
					stack[len(stack)-1].text += text
				}
			}
		}
	}

	if root == nil {
		return nil, fmt.Errorf("no root element found in XML")
	}
	return root, nil
}

// nodeToValue converts an xmlNode tree to a Go value suitable for JSON marshaling.
//
// Leaf nodes (no children) return a coerced scalar (bool, float64, or string).
// Nodes whose children include a <size> element are treated as list containers:
// the non-<size> children are collected into a []any slice.
// Other nodes return a map[string]any where repeated child names become arrays.
func nodeToValue(node *xmlNode) any {
	if len(node.children) == 0 {
		return coerceValue(node.text)
	}

	// Detect list container: a node with a <size> child is a collection.
	hasSize := false
	for _, child := range node.children {
		if child.name == "size" {
			hasSize = true
			break
		}
	}

	if hasSize {
		var items []any
		for _, child := range node.children {
			if child.name == "size" {
				continue
			}
			items = append(items, nodeToValue(child))
		}
		if items == nil {
			items = []any{}
		}
		return items
	}

	// Regular object: group children by element name.
	groups := make(map[string][]*xmlNode)
	var order []string
	for _, child := range node.children {
		if _, seen := groups[child.name]; !seen {
			order = append(order, child.name)
		}
		groups[child.name] = append(groups[child.name], child)
	}

	result := make(map[string]any, len(order))
	for _, name := range order {
		children := groups[name]
		if len(children) > 1 {
			arr := make([]any, len(children))
			for i, c := range children {
				arr[i] = nodeToValue(c)
			}
			result[name] = arr
		} else {
			result[name] = nodeToValue(children[0])
		}
	}
	return result
}

// coerceValue converts an XML text value to a typed Go value.
// "true"/"false" become bool, numeric strings become float64 (matching
// encoding/json behavior), everything else stays string.
// Strings with leading zeros like "007" are kept as strings to avoid
// losing formatting information.
func coerceValue(s string) any {
	if s == "" {
		return ""
	}
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}

	// Try numeric coercion, but skip strings with leading zeros (e.g. "007").
	numStr := s
	if len(numStr) > 0 && numStr[0] == '-' {
		numStr = numStr[1:]
	}
	if len(numStr) == 0 {
		return s
	}
	if len(numStr) > 1 && numStr[0] == '0' && numStr[1] != '.' {
		return s // Leading zero — keep as string
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

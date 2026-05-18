package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Format string

const (
	Human Format = "human"
	JSON  Format = "json"
	Plain Format = "plain"
)

func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func Project(v any, path string) (any, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return v, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var cur any
	if err := json.Unmarshal(raw, &cur); err != nil {
		return nil, err
	}
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			return nil, fmt.Errorf("invalid output path %q", path)
		}
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				return nil, fmt.Errorf("output path %q not found at %q", path, part)
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, fmt.Errorf("output path %q has invalid index %q", path, part)
			}
			cur = node[idx]
		default:
			return nil, fmt.Errorf("output path %q cannot descend into %q", path, part)
		}
	}
	return cur, nil
}

func WritePlainRows(w io.Writer, rows [][]string) error {
	for _, row := range rows {
		for i, col := range row {
			if i > 0 {
				if _, err := fmt.Fprint(w, "\t"); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprint(w, sanitizePlainCell(col)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprint(w, "\n"); err != nil {
			return err
		}
	}
	return nil
}

func sanitizePlainCell(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

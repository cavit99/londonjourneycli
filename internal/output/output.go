package output

import (
	"encoding/json"
	"fmt"
	"io"
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

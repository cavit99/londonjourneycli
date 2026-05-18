package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWritePlainRows(t *testing.T) {
	var b bytes.Buffer
	if err := WritePlainRows(&b, [][]string{{"a", "b"}, {"c", "d\ne\tf"}}); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != "a\tb\nc\td e f\n" {
		t.Fatalf("unexpected plain rows: %q", got)
	}
}

func TestWriteJSON(t *testing.T) {
	var b bytes.Buffer
	if err := WriteJSON(&b, map[string]string{"status": "ok"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "\"status\": \"ok\"") {
		t.Fatalf("unexpected json: %s", b.String())
	}
}

func TestProject(t *testing.T) {
	value := map[string]any{
		"lines": []map[string]any{{
			"name": "Victoria",
			"states": []map[string]any{{
				"severity": "Good Service",
			}},
		}},
	}

	got, err := Project(value, "lines.0.states.0.severity")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Good Service" {
		t.Fatalf("projection=%#v", got)
	}

	if got, err := Project(value, ""); err != nil || got == nil {
		t.Fatalf("empty projection got=%#v err=%v", got, err)
	}
}

func TestProjectPreservesLargeInteger(t *testing.T) {
	got, err := Project(map[string]any{"id": int64(9007199254740993)}, "id")
	if err != nil {
		t.Fatal(err)
	}
	number, ok := got.(json.Number)
	if !ok {
		t.Fatalf("expected json.Number, got %T %[1]v", got)
	}
	if number.String() != "9007199254740993" {
		t.Fatalf("large integer lost precision: %s", number.String())
	}
	var b bytes.Buffer
	if err := WriteJSON(&b, got); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(b.String()) != "9007199254740993" {
		t.Fatalf("projected JSON lost precision: %s", b.String())
	}
}

func TestProjectErrors(t *testing.T) {
	value := map[string]any{"lines": []any{map[string]any{"name": "Victoria"}}}
	cases := []string{
		"lines.1.name",
		"lines.name",
		"missing",
		"lines.bad",
		"lines.0.name.extra",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			if _, err := Project(value, path); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

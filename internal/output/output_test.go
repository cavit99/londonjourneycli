package output

import (
	"bytes"
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

package render_test

import (
	"bytes"
	"testing"

	"github.com/fleveque/quantic-cli/internal/render"
)

func TestJSON(t *testing.T) {
	var buf bytes.Buffer
	v := struct {
		Name   string `json:"name"`
		Symbol string `json:"symbol"`
	}{Name: "AT&T <Inc>", Symbol: "T"}

	if err := render.JSON(&buf, v); err != nil {
		t.Fatalf("JSON() returned error: %v", err)
	}

	want := "{\n  \"name\": \"AT&T <Inc>\",\n  \"symbol\": \"T\"\n}\n"
	if got := buf.String(); got != want {
		t.Errorf("JSON() wrote\n%s\nwant\n%s", got, want)
	}
}

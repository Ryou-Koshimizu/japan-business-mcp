package testcases

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

var update = flag.Bool("update", false, "web/examples.json を書き直す")

// TestWebExamplesUpToDate は、遊び場の例（web/examples.json）が試験の例と同じであることを確かめる。
func TestWebExamplesUpToDate(t *testing.T) {
	type ex struct {
		Name  string         `json:"name"`
		Tool  string         `json:"tool"`
		Label string         `json:"label"`
		Args  map[string]any `json:"args"`
	}
	list := make([]ex, 0, len(Cases))
	for _, c := range Cases {
		if c.Label == "" {
			t.Fatalf("%s に表示名が無い", c.Name)
		}
		list = append(list, ex{c.Name, c.Tool, c.Label, c.Args})
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "web", "examples.json")
	if *update {
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path) // #nosec G304 -- リポジトリ内の固定のファイル
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, b) {
		t.Fatal("web/examples.json が古い（go test ./internal/testcases -update）")
	}
}

// どのゴールデンにも、空の日付（0001-01-01）が混ざっていない。
func TestGoldenHasNoZeroDate(t *testing.T) {
	for _, c := range Cases {
		b, err := os.ReadFile(Path(c.Name)) // #nosec G304 -- リポジトリ内の固定のファイル
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte("0001-01-01")) {
			t.Errorf("%s に空の日付が出ている", c.Name)
		}
	}
}

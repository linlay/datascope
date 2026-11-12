package platform_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"datascope-server/internal/ai"
)

// Set DATASCOPE_ASSISTANT_BROWSER_NODE to a Node executable after building web/.
func TestAssistantBrowser(t *testing.T) {
	node := os.Getenv("DATASCOPE_ASSISTANT_BROWSER_NODE")
	if node == "" {
		t.Skip("optional Playwright acceptance test")
	}
	f, business := setupAction(t, false)
	db, err := f.c.Get("default")
	must(t, err)
	def := db.Actions[0]
	broken := def
	broken.Name = "needs_repair"
	broken.Title = "Needs repair"
	broken.Script = "function run(args, ctx) {return missingHelper(args)}"
	db.Actions = append(db.Actions, broken)
	must(t, f.c.Save(f.ctx, *db))
	f.app.Model = ai.Func(func(_ context.Context, r ai.Request) (ai.Response, error) {
		prompt := r.Messages[0].Content
		var out any
		switch {
		case strings.Contains(prompt, "actual trial summary"):
			diagnosis := map[string]string{"explanation": "Trial diagnosis: " + r.Messages[1].Content}
			if strings.Contains(r.Messages[1].Content, "undefined_javascript_reference") {
				diagnosis["repair"] = "Fix undefined helper while preserving ownership checks"
			}
			out = diagnosis
		case strings.Contains(prompt, "synthetic example"):
			out = map[string]any{"arguments": map[string]any{"id": 1, "name": "browser preview"}, "expected": "One owned row"}
		default:
			var data struct {
				Intent       string
				Conversation []ai.Message
			}
			if err := json.Unmarshal([]byte(r.Messages[1].Content), &data); err != nil {
				return ai.Response{}, err
			}
			if data.Intent == "Keep the same ownership checks" && (len(data.Conversation) != 2 || !strings.Contains(data.Conversation[0].Content, "owner_id")) {
				return ai.Response{}, ai.Invalid
			}
			out = map[string]any{"parameters": def.Parameters, "script": def.Script, "summary": "Check visibility and ownership before updating.", "explanation": "Update only the owned item and verify one affected row."}
		}
		b, e := json.Marshal(out)
		return ai.Response{Text: string(b)}, e
	})
	root := filepath.Join("..", "..")
	ui := http.FileServer(http.Dir(filepath.Join(root, "web", "dist")))
	mux := http.NewServeMux()
	backend := api(t, f)
	mux.Handle("/api/", backend.Config.Handler)
	mux.Handle("/", ui)
	server := httptest.NewServer(mux)
	defer server.Close()
	command := exec.CommandContext(t.Context(), node, filepath.Join(root, "scripts", "test-action-assistant-browser.cjs"), server.URL)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	must(t, err)
	rows, err := f.st.Query(business, "SELECT name FROM items WHERE id=1", 1)
	must(t, err)
	if rows.Rows[0]["name"] != "one" {
		t.Fatal("browser trial committed")
	}
}

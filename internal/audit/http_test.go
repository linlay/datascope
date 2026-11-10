package audit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPRequestAuditIdentityAndArguments(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	handler := HTTPHandler(s, "database-mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		UpdateRequest(r.Context(), "sales", 0)
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		SetRequestBusinessIdentity(r.Context(), 17, 25, "employee")
	}))
	for _, authenticated := range []bool{true, false} {
		body := `{"method":"tools/call","params":{"name":"query","arguments":{"sql":"select :arg_x","user_id":777,"max_rows":12,"password":"secret-value","parameters":{"arg_x":"secret-value"}}}}`
		r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
		if authenticated {
			r.Header.Set("Authorization", "secret-value")
		}
		handler.ServeHTTP(httptest.NewRecorder(), r)
		events, err := s.List(t.Context(), "sales", "", 1, 0)
		if err != nil || len(events) != 1 {
			t.Fatalf("missing event: %v %v", events, err)
		}
		event := events[0]
		if authenticated && (event.BusinessAccountID != "17" || event.SubjectID != 25 || event.BusinessUsername != "employee") {
			t.Fatalf("wrong identity: %+v", event)
		}
		if !authenticated && (event.SubjectID != 0 || event.BusinessUsername != "" || event.BusinessAccountID != "" || event.Status != "failed") {
			t.Fatalf("unverified identity: %+v", event)
		}
		raw, _ := json.Marshal(event)
		if event.ActorID != 0 || event.Tool != "query" || event.OriginalSQL != "select :arg_x" || !strings.Contains(string(raw), `"max_rows":12`) || strings.Contains(string(raw), "secret-value") {
			t.Fatalf("unexpected audit record: %s", raw)
		}
	}
}

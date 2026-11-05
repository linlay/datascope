package adminapi

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"datascope-server/internal/application"
)

func TestBusinessOAuthCallbackPage(t *testing.T) {
	app := &application.Service{WebURL: "http://127.0.0.1:5173"}
	for _, tt := range []struct{ name, code, authError, heading string }{
		{"complete", "test-code", "", "身份验证完成"},
		{"denied", "", "access_denied", "未完成授权"},
		{"missing code", "", "", "未完成授权"},
		{"escaped code", `</script><script>alert(1)</script>`, "", "身份验证完成"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			query := url.Values{"code": {tt.code}, "state": {"original-state"}, "error": {tt.authError}}
			response := httptest.NewRecorder()
			businessOAuthCallback(app, response, httptest.NewRequest("GET", "/api/action-oauth/callback?"+query.Encode(), nil))
			body := response.Body.String()
			for _, want := range []string{tt.heading, "返回原页面", `href="http://127.0.0.1:5173"`, `const origin = "http://127.0.0.1:5173"`, "original-state", "window.opener.postMessage(result, origin)", "window.opener.focus()", "window.close()"} {
				if !strings.Contains(body, want) {
					t.Fatalf("missing callback content %q: %s", want, body)
				}
			}
			if strings.Contains(body, `<script>alert(1)</script>`) {
				t.Fatal("unescaped authorization code")
			}
			if response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Header().Get("Content-Security-Policy"), "style-src 'unsafe-inline'") {
				t.Fatal("missing callback security headers")
			}
		})
	}
}

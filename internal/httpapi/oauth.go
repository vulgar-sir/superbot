package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleOAuthStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"providers": s.connectors.OAuthProviders(),
	})
}

func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	provider := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "provider")))
	target, err := s.connectors.OAuthStart(r.Context(), accountFrom(r), provider)
	if err != nil {
		writeError(w, http.StatusBadRequest, "oauth_not_configured", err.Error())
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "provider")))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		writeOAuthResult(w, provider, fmt.Errorf("missing code or state"))
		return
	}
	if err := s.connectors.OAuthComplete(r.Context(), accountFrom(r), provider, code, state); err != nil {
		writeOAuthResult(w, provider, err)
		return
	}
	writeOAuthResult(w, provider, nil)
}

func writeOAuthResult(w http.ResponseWriter, provider string, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		payload, _ := json.Marshal(err.Error())
		fmt.Fprintf(w, `<!doctype html><html><body style="font-family:system-ui;padding:32px;background:#fcfcfc;color:#1c1c1c">
<h3>连接失败</h3><p style="color:#e72e3b">%s</p>
<script>if(window.opener){window.opener.postMessage({type:'superbot-oauth-error',provider:%q,error:%s},'*');}</script>
</body></html>`, err.Error(), provider, payload)
		return
	}
	fmt.Fprintf(w, `<!doctype html><html><body style="font-family:system-ui;padding:32px;background:#fcfcfc;color:#1c1c1c">
<h3>已连接</h3><p>可以关闭此窗口，返回 superbot。</p>
<script>
const payload={type:'superbot-oauth-connected',provider:%q};
if(window.opener){window.opener.postMessage(payload,'*');setTimeout(()=>window.close(),200);}else{setTimeout(()=>location.href='/',400);}
</script>
</body></html>`, provider)
}

func (s *Server) handleOAuthMockAuthorize(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("SUPERBOT_OAUTH_MOCK") != "1" {
		http.NotFound(w, r)
		return
	}
	redirectURI := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")
	clientID := r.URL.Query().Get("client_id")
	if redirectURI == "" || state == "" || clientID == "" {
		http.Error(w, "missing oauth parameters", http.StatusBadRequest)
		return
	}
	separator := "?"
	if strings.Contains(redirectURI, "?") {
		separator = "&"
	}
	target := redirectURI + separator + "code=mock-code&state=" + state
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html><body style="font-family:system-ui;padding:32px;background:#fcfcfc;color:#1c1c1c">
<h3>superbot OAuth 模拟授权</h3>
<p>应用 <code>%s</code> 请求访问此账户。</p>
<p><a href="%s" style="display:inline-block;background:#1c1c1c;color:#fff;padding:10px 18px;border-radius:8px;text-decoration:none">允许</a></p>
</body></html>`, clientID, target)
}

func (s *Server) handleOAuthMockToken(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("SUPERBOT_OAUTH_MOCK") != "1" {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": "mock-oauth-token",
		"token_type":   "bearer",
	})
}

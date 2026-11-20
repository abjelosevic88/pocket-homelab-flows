package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-flows/internal/config"
)

// moveLinks starts Flows behind a fake Core whose pcx_<who>_<export|import|read>
// tokens are Pocket Move links acting for alice@ or bob@.
func moveLinks(t *testing.T) *app {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		if len(parts) != 2 {
			io.WriteString(w, `{"active":false}`)
			return
		}
		scope := map[string]string{"export": core.ScopeMoveExport, "import": core.ScopeMoveImport, "read": "flows:read"}[parts[1]]
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"flows","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"move","name":"Pocket Move"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
}

func (a *app) raw(tok, method, path, ctype string, body []byte) (int, []byte) {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func (a *app) moveImport(tok, path string, data []byte) map[string]any {
	a.t.Helper()
	code, out := a.raw(tok, "POST", path, "application/json", data)
	if code != 200 {
		a.t.Fatalf("POST %s → %d %s", path, code, out)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	return m
}

func TestMoveLinkScopes(t *testing.T) {
	a := moveLinks(t)
	exp, imp, read := "pcx_bob_export", "pcx_bob_import", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.json", 200},
		{exp, "GET", "/api/v1/export.csv", 200},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "POST", "/api/v1/import/n8n", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{exp, "GET", "/api/v1/flows", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.json", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{imp, "POST", "/api/v1/flows", 403},
		{imp, "POST", "/api/v1/recipes/x", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.json", 403},
	} {
		if code, body := a.raw(c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := a.raw(exp, "GET", core.PortablePath, "", nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 2 || len(p.Imports) != 2 || p.Exports[0].Kind != core.KindPocket || p.Imports[0].Form != "" {
		t.Fatalf("portable: %s", body)
	}
}

// Move brings Alice's flows to Bob: each import adds once, and nothing of one
// person shows up for the other.
func TestMoveImportsAddOnce(t *testing.T) {
	a := moveLinks(t)
	a.ok(a.alice, "POST", "/api/v1/flows", map[string]any{"name": "Water the plants", "trigger_kind": "manual",
		"steps": []map[string]any{{"kind": "wait", "config": map[string]string{"amount": "1", "unit": "minutes"}}}})

	code, export := a.raw("pcx_alice_export", "GET", "/api/v1/export.json", "", nil)
	if code != 200 || !strings.Contains(string(export), "Water the plants") {
		t.Fatalf("alice's export: %d %s", code, export)
	}
	if _, bob := a.raw("pcx_bob_export", "GET", "/api/v1/export.json", "", nil); strings.Contains(string(bob), "plants") {
		t.Fatalf("bob's export holds alice's flow: %s", bob)
	}

	first := a.moveImport("pcx_bob_import", "/api/v1/import", export)
	second := a.moveImport("pcx_bob_import", "/api/v1/import", export)
	if numOf(first, "flows") != 1 || numOf(second, "flows") != 0 || numOf(second, "existing") != 1 {
		t.Fatalf("pocket imports: %v then %v", first, second)
	}
	wf := []byte(`{"id":"wf1","name":"Ping","nodes":[{"name":"Go","type":"n8n-nodes-base.manualTrigger","parameters":{}},` +
		`{"name":"Ping","type":"n8n-nodes-base.httpRequest","parameters":{"url":"https://example.com/"}}],"connections":{"Go":{"main":[[{"node":"Ping"}]]}}}`)
	n1 := a.moveImport("pcx_bob_import", "/api/v1/import/n8n", wf)
	n2 := a.moveImport("pcx_bob_import", "/api/v1/import/n8n", wf)
	if numOf(n1, "created") != 1 || numOf(n2, "created") != 0 || numOf(n2, "existing") != 1 {
		t.Fatalf("n8n imports: %v then %v", n1, n2)
	}

	_, _, bob := a.call(a.bob, "GET", "/api/v1/flows", nil)
	_, _, alice := a.call(a.alice, "GET", "/api/v1/flows", nil)
	if len(bob) != 2 || len(alice) != 1 {
		t.Fatalf("flows: bob %v, alice %v", bob, alice)
	}
	for _, f := range bob {
		if f.(map[string]any)["enabled"] != false {
			t.Fatalf("an imported flow is on: %v", f)
		}
	}
}

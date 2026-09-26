package syncd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/huangzheng2016/eTerm/internal/relay"
	etersync "github.com/huangzheng2016/eTerm/internal/sync"
)

func webServer(t *testing.T, apiKey string, tlsEnabled bool) (*httptest.Server, *Engine, *PeerRegistry, *WebAuth) {
	t.Helper()
	engine := testEngine(t)
	peers := NewPeerRegistry()
	web := NewWebAuth(engine, peers, tlsEnabled)
	srv := httptest.NewServer(newHTTPHandler(engine, apiKey, peers, web))
	t.Cleanup(srv.Close)
	return srv, engine, peers, web
}

func webEvict(t *testing.T, web *WebAuth, cookie *http.Cookie) {
	t.Helper()
	web.mu.Lock()
	delete(web.cache, hashWebToken(cookie.Value))
	web.mu.Unlock()
}

func webPostLogin(t *testing.T, baseURL, password string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"password": password})
	req, err := http.NewRequest("POST", baseURL+"/api/v1/web/login", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func webSessionCookie(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("login status = %d body = %s", resp.StatusCode, body)
	}
	for _, c := range resp.Cookies() {
		if c.Name == webCookieName {
			return c
		}
	}
	t.Fatal("no eterm_web cookie")
	return nil
}

func webDo(t *testing.T, method, url string, cookie *http.Cookie, extra http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, vs := range extra {
		req.Header[k] = vs
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func webMe(t *testing.T, baseURL string, cookie *http.Cookie) (int, string) {
	t.Helper()
	resp := webDo(t, "GET", baseURL+"/api/v1/web/me", cookie, nil)
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func webRegisterPeer(peers *PeerRegistry, tenant, id string) {
	peers.Register(tenant, PeerInfo{ID: id, Name: id}, nil)
}

func TestWebLoginSetsCookieAndMe(t *testing.T) {
	srv, _, peers, _ := webServer(t, "secret", true)
	tenant := etersync.TenantIDFromPassphrase("pw-a")
	webRegisterPeer(peers, tenant, "peer-a")

	resp := webPostLogin(t, srv.URL, "pw-a")
	cookie := webSessionCookie(t, resp)
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("cookie attrs = %+v", cookie)
	}
	if cookie.MaxAge != int(webSessionMaxTTL.Seconds()) {
		t.Fatalf("cookie max-age = %d", cookie.MaxAge)
	}
	var loginBody struct {
		Tenant    string    `json:"tenant"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginBody); err != nil {
		t.Fatal(err)
	}
	if loginBody.Tenant != tenant {
		t.Fatalf("tenant = %q want %q", loginBody.Tenant, tenant)
	}

	status, body := webMe(t, srv.URL, cookie)
	if status != 200 {
		t.Fatalf("me status = %d body = %s", status, body)
	}
	var me struct {
		Tenant string     `json:"tenant"`
		Peers  []PeerInfo `json:"peers"`
		Hosts  []HostMeta `json:"hosts"`
	}
	if err := json.Unmarshal([]byte(body), &me); err != nil {
		t.Fatal(err)
	}
	if me.Tenant != tenant || len(me.Peers) != 1 || me.Peers[0].ID != "peer-a" || me.Hosts == nil {
		t.Fatalf("me = %+v", me)
	}
}

func TestWebLoginRejectsUnknownTenant(t *testing.T) {
	srv, _, peers, _ := webServer(t, "secret", true)
	webRegisterPeer(peers, etersync.TenantIDFromPassphrase("right"), "peer-a")
	for _, pw := range []string{"wrong", "right ", ""} {
		resp := webPostLogin(t, srv.URL, pw)
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusUnauthorized || strings.TrimSpace(string(body)) != "invalid credentials" {
			t.Fatalf("password %q: status = %d body = %q", pw, resp.StatusCode, body)
		}
	}
}

func TestWebLoginKnownByHostRecord(t *testing.T) {
	srv, engine, _, _ := webServer(t, "secret", true)
	tenant := etersync.TenantIDFromPassphrase("pw-host")
	if _, err := engine.Push(tenant, []SyncEntry{{
		SyncID: "h1", Type: "host",
		Meta:      `{"sync_id":"h1","alias":"web","hostname":"w.example","port":22,"username":"root"}`,
		DeviceID:  "d",
		UpdatedAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	webSessionCookie(t, webPostLogin(t, srv.URL, "pw-host"))
}

func TestWebLoginRequiresTLS(t *testing.T) {
	srv, _, peers, _ := webServer(t, "secret", false)
	webRegisterPeer(peers, etersync.TenantIDFromPassphrase("pw"), "peer-a")
	resp := webPostLogin(t, srv.URL, "pw")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestWebLoginRateLimitAndRecovery(t *testing.T) {
	oldWindow, oldBase, oldMax := webLoginRateWindow, webLoginBackoffBase, webLoginBackoffMax
	webLoginRateWindow = 100 * time.Millisecond
	webLoginBackoffBase = 150 * time.Millisecond
	webLoginBackoffMax = 600 * time.Millisecond
	t.Cleanup(func() { webLoginRateWindow, webLoginBackoffBase, webLoginBackoffMax = oldWindow, oldBase, oldMax })
	logs := captureSyncdLog(t)

	srv, _, peers, _ := webServer(t, "secret", true)
	webRegisterPeer(peers, etersync.TenantIDFromPassphrase("pw"), "peer-a")

	for i := 0; i < 5; i++ {
		if resp := webPostLogin(t, srv.URL, "bad"); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d", i, resp.StatusCode)
		}
	}
	if resp := webPostLogin(t, srv.URL, "bad"); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("6th attempt status = %d", resp.StatusCode)
	}
	if resp := webPostLogin(t, srv.URL, "pw"); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("correct password during lockout status = %d", resp.StatusCode)
	}
	if out := logs.String(); !strings.Contains(out, "web login locked") {
		t.Fatalf("lockout not logged: %q", out)
	}

	time.Sleep(200 * time.Millisecond)
	if resp := webPostLogin(t, srv.URL, "pw"); resp.StatusCode != http.StatusOK {
		t.Fatalf("recovery login status = %d", resp.StatusCode)
	}
}

func TestWebSessionSlidingExpiry(t *testing.T) {
	srv, engine, peers, web := webServer(t, "secret", true)
	tenant := etersync.TenantIDFromPassphrase("pw")
	webRegisterPeer(peers, tenant, "peer-a")
	cookie := webSessionCookie(t, webPostLogin(t, srv.URL, "pw"))

	old := time.Now().UTC().Add(-11 * time.Hour)
	engine.DB.Model(&WebSession{}).Where("tenant = ?", tenant).Updates(map[string]interface{}{
		"touched_at": old, "expires_at": old.Add(webSessionSlideTTL),
	})
	webEvict(t, web, cookie)
	if status, body := webMe(t, srv.URL, cookie); status != 200 {
		t.Fatalf("me status = %d body = %s", status, body)
	}
	var entry WebSession
	if err := engine.DB.Where("tenant = ?", tenant).First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if d := time.Until(entry.ExpiresAt); d < 11*time.Hour || d > 12*time.Hour {
		t.Fatalf("sliding expires in %v, want ~12h", d)
	}
}

func TestWebSessionAbsoluteCap(t *testing.T) {
	srv, engine, peers, web := webServer(t, "secret", true)
	tenant := etersync.TenantIDFromPassphrase("pw")
	webRegisterPeer(peers, tenant, "peer-a")
	cookie := webSessionCookie(t, webPostLogin(t, srv.URL, "pw"))

	created := time.Now().UTC().Add(-(webSessionMaxTTL - time.Hour))
	engine.DB.Model(&WebSession{}).Where("tenant = ?", tenant).Updates(map[string]interface{}{
		"created_at": created,
		"touched_at": created.Add(time.Hour),
		"expires_at": created.Add(webSessionMaxTTL),
	})
	webEvict(t, web, cookie)
	if status, body := webMe(t, srv.URL, cookie); status != 200 {
		t.Fatalf("me status = %d body = %s", status, body)
	}
	var entry WebSession
	if err := engine.DB.Where("tenant = ?", tenant).First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if d := time.Until(entry.ExpiresAt); d < 50*time.Minute || d > 70*time.Minute {
		t.Fatalf("absolute-capped expires in %v, want ~1h", d)
	}
}

func TestWebSessionExpiredRejected(t *testing.T) {
	srv, engine, peers, web := webServer(t, "secret", true)
	tenant := etersync.TenantIDFromPassphrase("pw")
	webRegisterPeer(peers, tenant, "peer-a")
	cookie := webSessionCookie(t, webPostLogin(t, srv.URL, "pw"))

	engine.DB.Model(&WebSession{}).Where("tenant = ?", tenant).
		Update("expires_at", time.Now().UTC().Add(-time.Minute))
	webEvict(t, web, cookie)
	if status, _ := webMe(t, srv.URL, cookie); status != http.StatusUnauthorized {
		t.Fatalf("me status = %d", status)
	}
	var count int64
	engine.DB.Model(&WebSession{}).Where("tenant = ?", tenant).Count(&count)
	if count != 0 {
		t.Fatal("expired session not deleted")
	}
}

func TestWebLogout(t *testing.T) {
	srv, engine, peers, _ := webServer(t, "secret", true)
	tenant := etersync.TenantIDFromPassphrase("pw")
	webRegisterPeer(peers, tenant, "peer-a")
	cookie := webSessionCookie(t, webPostLogin(t, srv.URL, "pw"))

	resp := webDo(t, "POST", srv.URL+"/api/v1/web/logout", cookie, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d", resp.StatusCode)
	}
	cleared := false
	for _, c := range resp.Cookies() {
		if c.Name == webCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("logout did not clear cookie")
	}
	if status, _ := webMe(t, srv.URL, cookie); status != http.StatusUnauthorized {
		t.Fatalf("me after logout status = %d", status)
	}
	var count int64
	engine.DB.Model(&WebSession{}).Where("tenant = ?", tenant).Count(&count)
	if count != 0 {
		t.Fatal("session row not deleted on logout")
	}
}

func TestWebLogoutAllRevokesTenantSessions(t *testing.T) {
	srv, engine, peers, _ := webServer(t, "secret", true)
	tenant := etersync.TenantIDFromPassphrase("pw")
	webRegisterPeer(peers, tenant, "peer-a")
	c1 := webSessionCookie(t, webPostLogin(t, srv.URL, "pw"))
	c2 := webSessionCookie(t, webPostLogin(t, srv.URL, "pw"))

	resp := webDo(t, "POST", srv.URL+"/api/v1/web/logout-all", c1, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout-all status = %d", resp.StatusCode)
	}
	if status, _ := webMe(t, srv.URL, c1); status != http.StatusUnauthorized {
		t.Fatalf("me c1 status = %d", status)
	}
	if status, _ := webMe(t, srv.URL, c2); status != http.StatusUnauthorized {
		t.Fatalf("me c2 status = %d", status)
	}
	var count int64
	engine.DB.Model(&WebSession{}).Where("tenant = ?", tenant).Count(&count)
	if count != 0 {
		t.Fatal("session rows not deleted on logout-all")
	}
}

func TestWebAuthMiddlewareDualChannel(t *testing.T) {
	srv, _, peers, _ := webServer(t, "secret", true)
	tenant := etersync.TenantIDFromPassphrase("pw")
	webRegisterPeer(peers, tenant, "peer-a")

	if resp := webDo(t, "GET", srv.URL+"/api/v1/peers", nil, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no creds status = %d", resp.StatusCode)
	}
	bearer := http.Header{"Authorization": []string{"Bearer secret"}, "X-ETerm-Tenant": []string{"tenant-b"}}
	if resp := webDo(t, "GET", srv.URL+"/api/v1/peers", nil, bearer); resp.StatusCode != 200 {
		t.Fatalf("bearer status = %d", resp.StatusCode)
	}
	if resp := webDo(t, "GET", srv.URL+"/api/v1/peers", &http.Cookie{Name: webCookieName, Value: "bogus"}, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bogus cookie status = %d", resp.StatusCode)
	}

	cookie := webSessionCookie(t, webPostLogin(t, srv.URL, "pw"))
	resp := webDo(t, "GET", srv.URL+"/api/v1/peers", cookie, http.Header{"X-ETerm-Tenant": []string{"tenant-b"}})
	if resp.StatusCode != 200 {
		t.Fatalf("cookie status = %d", resp.StatusCode)
	}
	var body struct {
		Peers []PeerInfo `json:"peers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Peers) != 1 || body.Peers[0].ID != "peer-a" {
		t.Fatalf("peers = %+v, want session tenant peers (header must be ignored)", body.Peers)
	}
}

func TestWebCookieAuthWSClient(t *testing.T) {
	engine := testEngine(t)
	peers := NewPeerRegistry()
	srv := httptest.NewServer(NewHTTPHandlerWithTLS(engine, "secret", peers, true))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	password := "ws-pw"
	tenant := etersync.TenantIDFromPassphrase(password)
	bearer := http.Header{"Authorization": []string{"Bearer secret"}}

	daemon := relayDial(t, ctx, wsBase(srv), "/api/v1/ws/daemon", bearer)
	hello, _ := json.Marshal(relay.HelloPayload{Role: "daemon", Tenant: tenant, PeerID: "peer-ws", Name: "ws", Version: relay.ProtocolVersion})
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameHello, Payload: hello})

	deadline := time.Now().Add(2 * time.Second)
	for {
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/peers", nil)
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("X-ETerm-Tenant", tenant)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			var body struct {
				Peers []PeerInfo `json:"peers"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			if len(body.Peers) == 1 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("peer did not register")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cookie := webSessionCookie(t, webPostLogin(t, srv.URL, password))

	if _, _, err := websocket.Dial(ctx, wsBase(srv)+"/api/v1/ws/client", nil); err == nil {
		t.Fatal("ws client without creds accepted")
	}

	client := relayDial(t, ctx, wsBase(srv), "/api/v1/ws/client", http.Header{"Cookie": []string{cookie.Name + "=" + cookie.Value}})
	open, _ := json.Marshal(relay.OpenRequest{PeerID: "peer-ws", Target: "local"})
	writeFrame(t, ctx, client, relay.Frame{Type: relay.FrameOpen, StreamID: 7, Payload: open})
	expectFrame(t, ctx, daemon, relay.FrameOpen, 7)
}

package syncd

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	etersync "github.com/huangzheng2016/eTerm/internal/sync"
)

const webCookieName = "eterm_web"

var (
	webSessionSlideTTL        = 12 * time.Hour
	webSessionMaxTTL          = 7 * 24 * time.Hour
	webSessionPersistInterval = time.Minute
	webLoginRateWindow        = time.Minute
	webLoginRateCount         = 5
	webLoginBackoffBase       = 30 * time.Second
	webLoginBackoffMax        = time.Hour
	webLoginSweepThreshold    = 1024
)

type WebSession struct {
	ID        string `gorm:"primaryKey"`
	TokenHash string `gorm:"uniqueIndex;not null;size:64"`
	Tenant    string `gorm:"index;not null;default:''"`
	CreatedAt time.Time
	TouchedAt time.Time
	ExpiresAt time.Time `gorm:"index"`
}

func (e *Engine) CleanupExpiredWebSessions() error {
	return e.DB.Where("expires_at <= ?", time.Now().UTC()).Delete(&WebSession{}).Error
}

type tenantCtxKey struct{}

func tenantFromContext(r *http.Request) string {
	tenant, _ := r.Context().Value(tenantCtxKey{}).(string)
	return tenant
}

func hashWebToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type webSessionCache struct {
	tenant      string
	createdAt   time.Time
	expiresAt   time.Time
	persistedAt time.Time
	revoked     bool
}

type loginLimiterEntry struct {
	windowStart time.Time
	attempts    int
	consecFails int
	lockedUntil time.Time
}

type WebAuth struct {
	engine *Engine
	peers  *PeerRegistry
	tls    bool

	mu     sync.Mutex
	cache  map[string]*webSessionCache
	limits map[string]*loginLimiterEntry
}

func NewWebAuth(engine *Engine, peers *PeerRegistry, tlsEnabled bool) *WebAuth {
	return &WebAuth{
		engine: engine,
		peers:  peers,
		tls:    tlsEnabled,
		cache:  make(map[string]*webSessionCache),
		limits: make(map[string]*loginLimiterEntry),
	}
}

func (a *WebAuth) sessionTenant(r *http.Request) (string, bool) {
	c, err := r.Cookie(webCookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	return a.validateToken(c.Value)
}

func (a *WebAuth) createSession(tenant string) (string, time.Time, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", time.Time{}, err
	}
	token := hex.EncodeToString(b[:])
	var idb [16]byte
	if _, err := rand.Read(idb[:]); err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	entry := &WebSession{
		ID:        "wbs_" + hex.EncodeToString(idb[:]),
		TokenHash: hashWebToken(token),
		Tenant:    tenant,
		CreatedAt: now,
		TouchedAt: now,
		ExpiresAt: now.Add(webSessionSlideTTL),
	}
	if err := a.engine.DB.Create(entry).Error; err != nil {
		return "", time.Time{}, err
	}
	a.mu.Lock()
	a.cache[entry.TokenHash] = &webSessionCache{tenant: tenant, createdAt: now, expiresAt: entry.ExpiresAt, persistedAt: now}
	a.mu.Unlock()
	return token, entry.ExpiresAt, nil
}

func (a *WebAuth) touchExpiry(createdAt, now time.Time) time.Time {
	expires := now.Add(webSessionSlideTTL)
	if max := createdAt.Add(webSessionMaxTTL); expires.After(max) {
		expires = max
	}
	return expires
}

func (a *WebAuth) validateToken(token string) (string, bool) {
	hash := hashWebToken(token)
	now := time.Now().UTC()

	a.mu.Lock()
	if c, ok := a.cache[hash]; ok {
		if c.revoked {
			a.mu.Unlock()
			return "", false
		}
		if !c.expiresAt.After(now) {
			delete(a.cache, hash)
			a.mu.Unlock()
			a.deleteSession(hash)
			return "", false
		}
		c.expiresAt = a.touchExpiry(c.createdAt, now)
		tenant := c.tenant
		expires := c.expiresAt
		persist := now.Sub(c.persistedAt) >= webSessionPersistInterval
		if persist {
			c.persistedAt = now
		}
		a.mu.Unlock()
		if persist {
			a.touchSession(hash, now, expires)
		}
		return tenant, true
	}
	a.mu.Unlock()

	var entry WebSession
	if err := a.engine.DB.Where("token_hash = ?", hash).First(&entry).Error; err != nil {
		return "", false
	}
	if !entry.ExpiresAt.After(now) {
		a.deleteSession(hash)
		return "", false
	}
	expires := a.touchExpiry(entry.CreatedAt, now)
	persist := now.Sub(entry.TouchedAt) >= webSessionPersistInterval
	c := &webSessionCache{tenant: entry.Tenant, createdAt: entry.CreatedAt, expiresAt: expires, persistedAt: entry.TouchedAt}
	if persist {
		c.persistedAt = now
	}
	a.mu.Lock()
	a.cache[hash] = c
	a.mu.Unlock()
	if persist {
		a.touchSession(hash, now, expires)
	}
	return entry.Tenant, true
}

func (a *WebAuth) touchSession(hash string, now, expires time.Time) {
	a.engine.DB.Model(&WebSession{}).Where("token_hash = ?", hash).
		Updates(map[string]interface{}{"touched_at": now, "expires_at": expires})
}

func (a *WebAuth) deleteSession(hash string) {
	if err := a.engine.DB.Where("token_hash = ?", hash).Delete(&WebSession{}).Error; err != nil {
		log.Printf("syncd web session delete failed token=%s: %v", shortID(hash), err)
	}
}

func (a *WebAuth) revokeToken(token string) error {
	hash := hashWebToken(token)
	if err := a.engine.DB.Where("token_hash = ?", hash).Delete(&WebSession{}).Error; err != nil {
		log.Printf("syncd web session revoke failed token=%s: %v", shortID(hash), err)
		a.mu.Lock()
		if c := a.cache[hash]; c != nil {
			c.revoked = true
		}
		a.mu.Unlock()
		return err
	}
	a.mu.Lock()
	delete(a.cache, hash)
	a.mu.Unlock()
	return nil
}

func (a *WebAuth) revokeTenant(tenant string) error {
	if err := a.engine.DB.Where("tenant = ?", tenant).Delete(&WebSession{}).Error; err != nil {
		log.Printf("syncd web session revoke-all failed tenant=%s: %v", shortID(tenant), err)
		a.mu.Lock()
		for _, c := range a.cache {
			if c.tenant == tenant {
				c.revoked = true
			}
		}
		a.mu.Unlock()
		return err
	}
	a.mu.Lock()
	for h, c := range a.cache {
		if c.tenant == tenant {
			delete(a.cache, h)
		}
	}
	a.mu.Unlock()
	return nil
}

func (a *WebAuth) sweepExpiredLocked(now time.Time) {
	for k, e := range a.limits {
		if now.Sub(e.windowStart) >= webLoginRateWindow && !now.Before(e.lockedUntil) {
			delete(a.limits, k)
		}
	}
	for h, c := range a.cache {
		if !c.expiresAt.After(now) {
			delete(a.cache, h)
		}
	}
}

func (a *WebAuth) tenantKnown(tenant string) bool {
	if tenant == "" {
		return false
	}
	if len(a.peers.List(tenant)) > 0 {
		return true
	}
	var count int64
	a.engine.DB.Model(&SyncEntry{}).Where("tenant = ? AND type = ? AND deleted = ?", tenant, "host", false).Count(&count)
	return count > 0
}

func loginLimiterBackoff(consecFails int) time.Duration {
	backoff := webLoginBackoffBase
	for i := webLoginRateCount; i < consecFails && backoff < webLoginBackoffMax; i++ {
		backoff *= 2
	}
	if backoff > webLoginBackoffMax {
		backoff = webLoginBackoffMax
	}
	return backoff
}

func limiterLogKey(k string) string {
	if tenant, ok := strings.CutPrefix(k, "tenant:"); ok {
		return "tenant:" + shortID(tenant)
	}
	return k
}

func (a *WebAuth) allowLogin(keys ...string) bool {
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.limits) > webLoginSweepThreshold {
		a.sweepExpiredLocked(now)
	}
	for _, k := range keys {
		e := a.limits[k]
		if e == nil {
			continue
		}
		if now.Before(e.lockedUntil) {
			return false
		}
		if now.Sub(e.windowStart) < webLoginRateWindow && e.attempts >= webLoginRateCount {
			return false
		}
	}
	for _, k := range keys {
		e := a.limits[k]
		if e == nil {
			e = &loginLimiterEntry{windowStart: now}
			a.limits[k] = e
		}
		if now.Sub(e.windowStart) >= webLoginRateWindow {
			e.windowStart = now
			e.attempts = 0
		}
		e.attempts++
	}
	return true
}

func (a *WebAuth) recordLoginFailure(keys ...string) {
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, k := range keys {
		e := a.limits[k]
		if e == nil {
			e = &loginLimiterEntry{windowStart: now}
			a.limits[k] = e
		}
		e.consecFails++
		if e.consecFails >= webLoginRateCount {
			e.lockedUntil = now.Add(loginLimiterBackoff(e.consecFails))
			log.Printf("syncd web login locked %s consecutive_failures=%d until=%s",
				limiterLogKey(k), e.consecFails, e.lockedUntil.Format(time.RFC3339))
		}
	}
}

func (a *WebAuth) resetLogin(keys ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, k := range keys {
		delete(a.limits, k)
	}
}

// webLoginTLSOK reports whether the request arrived over TLS, either
// terminated directly (r.TLS) or at a reverse proxy (X-Forwarded-Proto).
func webLoginTLSOK(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (a *WebAuth) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !webLoginTLSOK(r) {
		http.Error(w, "web login requires TLS", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	keys := []string{"ip:" + ip}
	if body.Password != "" {
		keys = append(keys, "tenant:"+etersync.TenantIDFromPassphrase(body.Password))
	}
	if !a.allowLogin(keys...) {
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
		return
	}
	tenant := etersync.TenantIDFromPassphrase(body.Password)
	if !a.tenantKnown(tenant) {
		a.recordLoginFailure(keys...)
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	a.resetLogin(keys...)
	token, expires, err := a.createSession(tenant)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     webCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(webSessionMaxTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	json.NewEncoder(w).Encode(map[string]interface{}{"tenant": tenant, "expires_at": expires})
}

func (a *WebAuth) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(webCookieName); err == nil && c.Value != "" {
		if err := a.revokeToken(c.Value); err != nil {
			http.Error(w, "logout failed", http.StatusInternalServerError)
			return
		}
	}
	a.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (a *WebAuth) handleLogoutAll(w http.ResponseWriter, r *http.Request) {
	if err := a.revokeTenant(tenantFromContext(r)); err != nil {
		http.Error(w, "logout failed", http.StatusInternalServerError)
		return
	}
	a.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (a *WebAuth) handleMe(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromContext(r)
	hosts, err := a.engine.HostMetas(tenant)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tenant": tenant,
		"peers":  a.peers.List(tenant),
		"hosts":  hosts,
	})
}

func (a *WebAuth) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     webCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

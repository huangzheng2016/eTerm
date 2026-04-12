package security

import (
	"crypto/hmac"
	"sync"
	"time"
)

type MasterKeyManager struct {
	mu           sync.RWMutex
	key          *SecureBytes
	verifier     []byte
	salt         []byte
	lockTimeout  time.Duration
	lastActivity time.Time
	locked       bool
}

func NewMasterKeyManager(salt, verifier []byte, timeout time.Duration) *MasterKeyManager {
	return &MasterKeyManager{
		salt:         salt,
		verifier:     verifier,
		lockTimeout:  timeout,
		lastActivity: time.Now(),
		locked:       true,
	}
}

func (m *MasterKeyManager) Setup(password []byte) (salt, verifier []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, _ := GenerateSalt()
	derived := DeriveKey(password, s)
	v := ComputeVerifier(derived)

	m.salt = s
	m.verifier = v
	m.key = New(derived)
	m.locked = false
	m.lastActivity = time.Now()

	return s, v
}

func (m *MasterKeyManager) Unlock(password []byte) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	derived := DeriveKey(password, m.salt)
	v := ComputeVerifier(derived)

	if !hmac.Equal(v, m.verifier) {
		ClearBytes(derived)
		return false
	}

	m.key = New(derived)
	ClearBytes(derived)
	m.locked = false
	m.lastActivity = time.Now()

	return true
}

// VerifyPassword checks the password against the stored verifier without changing unlock state.
func (m *MasterKeyManager) VerifyPassword(password []byte) bool {
	m.mu.RLock()
	salt := m.salt
	verifier := m.verifier
	m.mu.RUnlock()

	derived := DeriveKey(password, salt)
	v := ComputeVerifier(derived)
	ok := hmac.Equal(v, verifier)
	ClearBytes(derived)
	return ok
}

// ReplaceAfterRotation installs new salt/verifier and key material after DB re-encryption.
func (m *MasterKeyManager) ReplaceAfterRotation(salt []byte, verifier []byte, key *SecureBytes) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.key != nil {
		m.key.Clear()
		m.key = nil
	}
	m.salt = append([]byte(nil), salt...)
	m.verifier = append([]byte(nil), verifier...)
	m.key = key
	m.locked = false
	m.lastActivity = time.Now()
}

func (m *MasterKeyManager) SetupNoPassword() (salt, verifier []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s := []byte("eterm-no-password")
	derived := DeriveKey([]byte(""), s)
	v := ComputeVerifier(derived)

	m.salt = s
	m.verifier = v
	m.key = New(derived)
	m.locked = false
	m.lastActivity = time.Now()

	return s, v
}

func (m *MasterKeyManager) UnlockNoPassword() {
	m.mu.Lock()
	defer m.mu.Unlock()

	derived := DeriveKey([]byte(""), m.salt)
	m.key = New(derived)
	ClearBytes(derived)
	m.locked = false
	m.lastActivity = time.Now()
}

func (m *MasterKeyManager) Lock() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.key != nil {
		m.key.Clear()
		m.key = nil
	}
	m.locked = true
}

func (m *MasterKeyManager) IsLocked() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.locked
}

func (m *MasterKeyManager) GetKey() *SecureBytes {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.key == nil {
		return nil
	}
	return New(m.key.Bytes())
}

func (m *MasterKeyManager) Touch() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastActivity = time.Now()
}

func (m *MasterKeyManager) CheckTimeout() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.locked {
		return true
	}

	if time.Since(m.lastActivity) > m.lockTimeout {
		if m.key != nil {
			m.key.Clear()
			m.key = nil
		}
		m.locked = true
		return true
	}

	return false
}

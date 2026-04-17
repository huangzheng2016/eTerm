package ssh

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jcmturner/gokrb5/v8/client"
	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/credentials"
	"github.com/jcmturner/gokrb5/v8/gssapi"
	"github.com/jcmturner/gokrb5/v8/keytab"
	"github.com/jcmturner/gokrb5/v8/spnego"
	"github.com/jcmturner/gokrb5/v8/types"
)

// gssAPIClient implements golang.org/x/crypto/ssh.GSSAPIClient using gokrb5.
type gssAPIClient struct {
	krb          *client.Client
	sessKey      types.EncryptionKey
	cleanupPaths []string
}

// InitSecContext builds a KRB5 AP_REQ token for the SSH server.
// target is "host@hostname" as provided by the SSH library.
func (g *gssAPIClient) InitSecContext(target string, token []byte, isGSSDelegCreds bool) ([]byte, bool, error) {
	// Convert "host@hostname" → SPN "host/hostname"
	spn := target
	if i := strings.Index(target, "@"); i >= 0 {
		spn = target[:i] + "/" + target[i+1:]
	}

	tkt, sessKey, err := g.krb.GetServiceTicket(spn)
	if err != nil {
		return nil, false, fmt.Errorf("kerberos: get service ticket for %q: %w", spn, err)
	}
	g.sessKey = sessKey

	var flags []int
	if isGSSDelegCreds {
		flags = append(flags, gssapi.ContextFlagDeleg)
	}
	flags = append(flags, gssapi.ContextFlagInteg, gssapi.ContextFlagMutual)

	krb5Token, err := spnego.NewKRB5TokenAPREQ(g.krb, tkt, sessKey, flags, nil)
	if err != nil {
		return nil, false, fmt.Errorf("kerberos: build AP_REQ: %w", err)
	}
	b, err := krb5Token.Marshal()
	if err != nil {
		return nil, false, fmt.Errorf("kerberos: marshal token: %w", err)
	}
	return b, false, nil
}

// GetMIC computes a MIC over the session binding data using the session key.
func (g *gssAPIClient) GetMIC(micField []byte) ([]byte, error) {
	mic, err := gssapi.NewInitiatorMICToken(micField, g.sessKey)
	if err != nil {
		return nil, fmt.Errorf("kerberos: GetMIC: %w", err)
	}
	b, err := mic.Marshal()
	if err != nil {
		return nil, fmt.Errorf("kerberos: marshal MIC: %w", err)
	}
	return b, nil
}

// DeleteSecContext releases the Kerberos client resources.
func (g *gssAPIClient) DeleteSecContext() error {
	if g.krb != nil {
		g.krb.Destroy()
	}
	return cleanupAll(g.cleanupPaths)
}

type resolvedCCache struct {
	path         string
	cleanupPaths []string
}

var (
	gssapiGOOS                = runtime.GOOS
	gssapiDefaultKrb5ConfPath = "/etc/krb5.conf"
	gssapiRunCommand          = func(name string, args ...string) ([]byte, error) {
		cmd := exec.Command(name, args...)
		return cmd.CombinedOutput()
	}
)

func loadKrb5Conf(path, realmHint string) (*config.Config, error) {
	if path == "" {
		path = os.Getenv("KRB5_CONFIG")
	}
	if path != "" {
		return loadKrb5ConfFile(path)
	}
	if _, err := os.Stat(gssapiDefaultKrb5ConfPath); err == nil {
		return loadKrb5ConfFile(gssapiDefaultKrb5ConfPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("kerberos: stat krb5.conf %q: %w", gssapiDefaultKrb5ConfPath, err)
	}
	return synthesizeKrb5Conf(realmHint)
}

func loadKrb5ConfFile(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("kerberos: load krb5.conf %q: %w", path, err)
	}
	return cfg, nil
}

func synthesizeKrb5Conf(realm string) (*config.Config, error) {
	realm = strings.TrimSpace(realm)
	if realm == "" {
		return nil, fmt.Errorf("kerberos: cannot synthesize krb5.conf without a realm")
	}
	cfg := config.New()
	cfg.LibDefaults.DefaultRealm = realm
	cfg.LibDefaults.DNSLookupKDC = true
	cfg.LibDefaults.DNSLookupRealm = true
	return cfg, nil
}

func resolveCCachePath(ccachePath string) (resolvedCCache, error) {
	if ccachePath == "" {
		ccachePath = os.Getenv("KRB5CCNAME")
	}
	if ccachePath == "" {
		if gssapiGOOS == "darwin" {
			return stageDarwinCCache("")
		}
		return resolvedCCache{path: fmt.Sprintf("/tmp/krb5cc_%d", os.Getuid())}, nil
	}
	switch scheme := credentialCacheScheme(ccachePath); scheme {
	case "":
		return resolvedCCache{path: ccachePath}, nil
	case "FILE":
		return resolvedCCache{path: trimFileScheme(ccachePath)}, nil
	case "API":
		if gssapiGOOS != "darwin" {
			return resolvedCCache{}, fmt.Errorf("kerberos: unsupported credential cache type %q", scheme)
		}
		return stageDarwinCCache(ccachePath)
	default:
		return resolvedCCache{}, fmt.Errorf("kerberos: unsupported credential cache type %q", scheme)
	}
}

func credentialCacheScheme(path string) string {
	i := strings.IndexByte(path, ':')
	if i <= 0 {
		return ""
	}
	prefix := path[:i]
	for _, r := range prefix {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return ""
		}
	}
	return strings.ToUpper(prefix)
}

func trimFileScheme(path string) string {
	if len(path) >= len("FILE:") && strings.EqualFold(path[:len("FILE:")], "FILE:") {
		return path[len("FILE:"):]
	}
	return path
}

func stageDarwinCCache(source string) (resolvedCCache, error) {
	tempDir, err := os.MkdirTemp("", "eterm-krb5cc-*")
	if err != nil {
		return resolvedCCache{}, fmt.Errorf("kerberos: create temp ccache dir: %w", err)
	}
	path := filepath.Join(tempDir, "ccache")
	var msgs []string
	for _, dest := range []string{"FILE:" + path, path} {
		args := []string{"copy_cred_cache"}
		if source != "" {
			args = append(args, source)
		}
		args = append(args, dest)
		out, runErr := gssapiRunCommand("kcc", args...)
		if runErr == nil {
			return resolvedCCache{path: path, cleanupPaths: []string{tempDir}}, nil
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = runErr.Error()
		}
		msgs = append(msgs, msg)
	}
	_ = os.RemoveAll(tempDir)
	if source == "" {
		return resolvedCCache{}, fmt.Errorf("kerberos: stage default mac credential cache with kcc: %s", strings.Join(msgs, "; "))
	}
	return resolvedCCache{}, fmt.Errorf("kerberos: stage mac credential cache %q with kcc: %s", source, strings.Join(msgs, "; "))
}

func cleanupAll(paths []string) error {
	var firstErr error
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := os.RemoveAll(path); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func realmFromPrincipal(principal string) string {
	i := strings.LastIndex(principal, "@")
	if i < 0 || i == len(principal)-1 {
		return ""
	}
	return strings.TrimSpace(principal[i+1:])
}

// NewGSSAPIFromCCache creates a GSSAPIClient using an existing credential cache (kinit).
func NewGSSAPIFromCCache(ccachePath, krb5ConfPath string) (*gssAPIClient, error) {
	resolved, err := resolveCCachePath(ccachePath)
	if err != nil {
		return nil, err
	}
	cc, err := credentials.LoadCCache(resolved.path)
	if err != nil {
		_ = cleanupAll(resolved.cleanupPaths)
		return nil, fmt.Errorf("kerberos: load ccache %q: %w", resolved.path, err)
	}
	cfg, err := loadKrb5Conf(krb5ConfPath, cc.DefaultPrincipal.Realm)
	if err != nil {
		_ = cleanupAll(resolved.cleanupPaths)
		return nil, err
	}
	cl, err := client.NewFromCCache(cc, cfg)
	if err != nil {
		_ = cleanupAll(resolved.cleanupPaths)
		return nil, fmt.Errorf("kerberos: create client from ccache: %w", err)
	}
	return &gssAPIClient{krb: cl, cleanupPaths: resolved.cleanupPaths}, nil
}

// NewGSSAPIFromKeytab creates a GSSAPIClient using a keytab file.
func NewGSSAPIFromKeytab(principal, keytabPath, krb5ConfPath string) (*gssAPIClient, error) {
	cfg, err := loadKrb5Conf(krb5ConfPath, realmFromPrincipal(principal))
	if err != nil {
		return nil, err
	}
	kt, err := keytab.Load(keytabPath)
	if err != nil {
		return nil, fmt.Errorf("kerberos: load keytab %q: %w", keytabPath, err)
	}
	// Split "user@REALM" into username and realm.
	user := principal
	realm := ""
	if i := strings.LastIndex(principal, "@"); i >= 0 {
		user = principal[:i]
		realm = principal[i+1:]
	}
	if realm == "" {
		realm = cfg.LibDefaults.DefaultRealm
	}
	if realm == "" {
		return nil, fmt.Errorf("kerberos: principal %q does not include a realm and no default realm is configured", principal)
	}
	cl := client.NewWithKeytab(user, realm, kt, cfg)
	if err := cl.Login(); err != nil {
		return nil, fmt.Errorf("kerberos: login with keytab: %w", err)
	}
	return &gssAPIClient{krb: cl}, nil
}

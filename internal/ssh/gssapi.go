package ssh

import (
	"fmt"
	"os"
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
	krb     *client.Client
	sessKey types.EncryptionKey
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
	return nil
}

func defaultKrb5ConfPath() string {
	if p := os.Getenv("KRB5_CONFIG"); p != "" {
		return p
	}
	return "/etc/krb5.conf"
}

func defaultCCachePath() string {
	if p := os.Getenv("KRB5CCNAME"); p != "" {
		// Strip "FILE:" prefix if present.
		return strings.TrimPrefix(p, "FILE:")
	}
	return fmt.Sprintf("/tmp/krb5cc_%d", os.Getuid())
}

func loadKrb5Conf(path string) (*config.Config, error) {
	if path == "" {
		path = defaultKrb5ConfPath()
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("kerberos: load krb5.conf %q: %w", path, err)
	}
	return cfg, nil
}

// NewGSSAPIFromCCache creates a GSSAPIClient using an existing credential cache (kinit).
func NewGSSAPIFromCCache(ccachePath, krb5ConfPath string) (*gssAPIClient, error) {
	cfg, err := loadKrb5Conf(krb5ConfPath)
	if err != nil {
		return nil, err
	}
	if ccachePath == "" {
		ccachePath = defaultCCachePath()
	}
	cc, err := credentials.LoadCCache(ccachePath)
	if err != nil {
		return nil, fmt.Errorf("kerberos: load ccache %q: %w", ccachePath, err)
	}
	cl, err := client.NewFromCCache(cc, cfg)
	if err != nil {
		return nil, fmt.Errorf("kerberos: create client from ccache: %w", err)
	}
	return &gssAPIClient{krb: cl}, nil
}

// NewGSSAPIFromKeytab creates a GSSAPIClient using a keytab file.
func NewGSSAPIFromKeytab(principal, keytabPath, krb5ConfPath string) (*gssAPIClient, error) {
	cfg, err := loadKrb5Conf(krb5ConfPath)
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
	cl := client.NewWithKeytab(user, realm, kt, cfg)
	if err := cl.Login(); err != nil {
		return nil, fmt.Errorf("kerberos: login with keytab: %w", err)
	}
	return &gssAPIClient{krb: cl}, nil
}

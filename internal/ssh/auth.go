package ssh

import (
	"fmt"
	"io"
	"net"
	"os"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// BuildAuthMethods returns auth methods and a slice of io.Closers that must be
// closed when the SSH session ends (e.g. the unix socket for ssh-agent).
func BuildAuthMethods(host *db.Host, key *db.SSHKey, masterKey *security.MasterKeyManager) ([]ssh.AuthMethod, []io.Closer, error) {
	switch host.AuthMethod {
	case "password":
		secKey := masterKey.GetKey()
		if secKey == nil {
			return nil, nil, fmt.Errorf("master key is locked")
		}
		defer secKey.Clear()

		passBytes, err := security.Decrypt(host.Password, secKey.Bytes())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to decrypt password: %w", err)
		}
		password := string(passBytes)
		security.ClearBytes(passBytes)

		return []ssh.AuthMethod{ssh.Password(password)}, nil, nil

	case "key":
		if key == nil {
			return nil, nil, fmt.Errorf("ssh key not provided")
		}
		signer, err := LoadPrivateKey(key, masterKey)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load private key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil, nil

	case "agent":
		sock := os.Getenv("SSH_AUTH_SOCK")
		if sock == "" {
			return nil, nil, fmt.Errorf("SSH_AUTH_SOCK not set")
		}
		conn, err := net.Dial("unix", sock)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to connect to ssh-agent: %w", err)
		}
		agentClient := agent.NewClient(conn)
		return []ssh.AuthMethod{ssh.PublicKeysCallback(agentClient.Signers)}, []io.Closer{conn}, nil

	case "interactive":
		secKey := masterKey.GetKey()
		if secKey == nil {
			return nil, nil, fmt.Errorf("master key is locked")
		}
		defer secKey.Clear()

		passBytes, err := security.Decrypt(host.Password, secKey.Bytes())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to decrypt password: %w", err)
		}
		password := string(passBytes)
		security.ClearBytes(passBytes)

		return []ssh.AuthMethod{ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = password
			}
			return answers, nil
		})}, nil, nil

	case "gssapi":
		gssClient, err := buildGSSAPIClient(host)
		if err != nil {
			return nil, nil, fmt.Errorf("gssapi auth: %w", err)
		}
		return []ssh.AuthMethod{ssh.GSSAPIWithMICAuthMethod(gssClient, host.Hostname)}, nil, nil

	default:
		return nil, nil, fmt.Errorf("unsupported auth method: %s", host.AuthMethod)
	}
}

func LoadPrivateKey(key *db.SSHKey, masterKey *security.MasterKeyManager) (ssh.Signer, error) {
	var keyData []byte

	switch key.StorageMode {
	case "database":
		secKey := masterKey.GetKey()
		if secKey == nil {
			return nil, fmt.Errorf("master key is locked")
		}
		defer secKey.Clear()

		decrypted, err := security.Decrypt(key.PrivateKeyData, secKey.Bytes())
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt private key data: %w", err)
		}
		keyData = decrypted

	case "file":
		data, err := os.ReadFile(key.PrivatePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read private key file: %w", err)
		}
		keyData = data

	default:
		return nil, fmt.Errorf("unsupported storage mode: %s", key.StorageMode)
	}

	defer security.ClearBytes(keyData)

	var signer ssh.Signer
	if key.Passphrase != "" {
		secKey := masterKey.GetKey()
		if secKey == nil {
			return nil, fmt.Errorf("master key is locked")
		}
		defer secKey.Clear()

		passphraseBytes, err := security.Decrypt(key.Passphrase, secKey.Bytes())
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt passphrase: %w", err)
		}
		defer security.ClearBytes(passphraseBytes)

		signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, passphraseBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key with passphrase: %w", err)
		}
	} else {
		var err error
		signer, err = ssh.ParsePrivateKey(keyData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
	}
	if key.CertificatePath == "" {
		return signer, nil
	}
	certData, err := os.ReadFile(key.CertificatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey(certData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}
	cert, ok := pub.(*ssh.Certificate)
	if !ok {
		return nil, fmt.Errorf("certificate is not an SSH certificate")
	}
	certSigner, err := ssh.NewCertSigner(cert, signer)
	if err != nil {
		return nil, fmt.Errorf("failed to build certificate signer: %w", err)
	}
	return certSigner, nil
}

// buildGSSAPIClient creates a gssAPIClient based on the host's GSSAPISource setting.
func buildGSSAPIClient(host *db.Host) (*gssAPIClient, error) {
	switch host.GSSAPISource {
	case "keytab":
		if host.GSSAPIKeytab == "" {
			return nil, fmt.Errorf("keytab path not configured")
		}
		if host.KrbPrincipal == "" {
			return nil, fmt.Errorf("kerberos principal not configured")
		}
		return NewGSSAPIFromKeytab(host.KrbPrincipal, host.GSSAPIKeytab, "")
	default: // "ccache" or ""
		return NewGSSAPIFromCCache("", "")
	}
}

func EnableAgentForwarding(client *ssh.Client, sess *ssh.Session) error {
	if client == nil || sess == nil {
		return fmt.Errorf("agent forwarding requires an SSH client and session")
	}
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return fmt.Errorf("SSH_AUTH_SOCK not set")
	}
	if err := agent.ForwardToRemote(client, sock); err != nil {
		return fmt.Errorf("failed to forward local ssh-agent: %w", err)
	}
	if err := agent.RequestAgentForwarding(sess); err != nil {
		return fmt.Errorf("failed to request agent forwarding: %w", err)
	}
	return nil
}

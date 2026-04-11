package keys

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

func GenerateED25519() (privateKeyPEM []byte, publicKey string, fingerprint string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to generate ed25519 key: %w", err)
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to create ssh public key: %w", err)
	}

	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to marshal private key: %w", err)
	}

	privateKeyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8,
	})

	publicKey = string(ssh.MarshalAuthorizedKey(sshPub))
	fingerprint = ssh.FingerprintSHA256(sshPub)

	return privateKeyPEM, publicKey, fingerprint, nil
}

func GenerateRSA(bits int) (privateKeyPEM []byte, publicKey string, fingerprint string, err error) {
	if bits <= 0 {
		bits = 4096
	}

	privKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to generate rsa key: %w", err)
	}

	privateKeyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})

	sshPub, err := ssh.NewPublicKey(&privKey.PublicKey)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to create ssh public key: %w", err)
	}

	publicKey = string(ssh.MarshalAuthorizedKey(sshPub))
	fingerprint = ssh.FingerprintSHA256(sshPub)

	return privateKeyPEM, publicKey, fingerprint, nil
}

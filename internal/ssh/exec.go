package ssh

import (
	"strings"

	"golang.org/x/crypto/ssh"
)

func RunCommand(client *ssh.Client, cmd string, stdin string) ([]byte, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	if stdin != "" {
		sess.Stdin = strings.NewReader(stdin)
	}
	return sess.CombinedOutput(cmd)
}

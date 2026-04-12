package sync

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/ssh"
)

type sshTransport struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	enc     *json.Encoder
	dec     *json.Decoder
	closers []io.Closer // from ConnectResult
}

// NewSSHTransport creates a sync transport over an existing SSH client.
// The caller is responsible for establishing the connection (e.g. via internalssh.Connect).
// closers are additional resources (agent conns, jump clients) closed on Close().
func NewSSHTransport(client *ssh.Client, closers []io.Closer, remoteBin, remoteDB string) (Transport, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh session: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, err
	}

	cmd := fmt.Sprintf("%s --stdio --db '%s'", remoteBin, strings.ReplaceAll(remoteDB, "'", "'\\''"))
	if err := session.Start(cmd); err != nil {
		session.Close()
		return nil, fmt.Errorf("ssh exec %q: %w", cmd, err)
	}

	t := &sshTransport{
		client:  client,
		session: session,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		closers: closers,
	}
	t.enc = json.NewEncoder(stdin)
	t.dec = json.NewDecoder(t.stdout)
	return t, nil
}

// stdio JSON-RPC types
type stdioRequest struct {
	Method  string       `json:"method"`
	Since   int64        `json:"since,omitempty"`
	Records []SyncRecord `json:"records,omitempty"`
}

type stdioResponse struct {
	OK       bool         `json:"ok,omitempty"`
	Records  []SyncRecord `json:"records,omitempty"`
	Revision int64        `json:"revision,omitempty"`
	Error    string       `json:"error,omitempty"`
}

func (t *sshTransport) call(req stdioRequest) (stdioResponse, error) {
	if err := t.enc.Encode(req); err != nil {
		return stdioResponse{}, err
	}
	var resp stdioResponse
	if err := t.dec.Decode(&resp); err != nil {
		return stdioResponse{}, err
	}
	if resp.Error != "" {
		return resp, fmt.Errorf("remote: %s", resp.Error)
	}
	return resp, nil
}

func (t *sshTransport) Ping() error {
	_, err := t.call(stdioRequest{Method: "ping"})
	return err
}

func (t *sshTransport) Pull(sinceRev int64) ([]SyncRecord, int64, error) {
	resp, err := t.call(stdioRequest{Method: "pull", Since: sinceRev})
	if err != nil {
		return nil, 0, err
	}
	return resp.Records, resp.Revision, nil
}

func (t *sshTransport) Push(records []SyncRecord) (int64, error) {
	resp, err := t.call(stdioRequest{Method: "push", Records: records})
	if err != nil {
		return 0, err
	}
	return resp.Revision, nil
}

func (t *sshTransport) Close() error {
	t.stdin.Close()
	t.session.Wait()
	t.session.Close()
	for _, c := range t.closers {
		c.Close()
	}
	return t.client.Close()
}

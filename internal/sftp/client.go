package sftp

import (
	"io"
	"os"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type FileInfo struct {
	Name    string
	Size    int64
	IsDir   bool
	ModTime time.Time
	Mode    os.FileMode
}

type Client struct {
	sftpClient *sftp.Client
	sshClient  *ssh.Client
	closers    []io.Closer
}

func NewClient(sshClient *ssh.Client) (*Client, error) {
	sc, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, err
	}
	return &Client{
		sftpClient: sc,
		sshClient:  sshClient,
	}, nil
}

// AddClosers attaches resources (jump host chain, agent socket) to be
// released with the client.
func (c *Client) AddClosers(cl ...io.Closer) {
	c.closers = append(c.closers, cl...)
}

func (c *Client) Close() error {
	err := c.sftpClient.Close()
	if c.sshClient != nil {
		_ = c.sshClient.Close()
	}
	for _, cl := range c.closers {
		if cl != nil {
			_ = cl.Close()
		}
	}
	return err
}

func (c *Client) List(path string) ([]FileInfo, error) {
	entries, err := c.sftpClient.ReadDir(path)
	if err != nil {
		return nil, err
	}
	files := make([]FileInfo, len(entries))
	for i, e := range entries {
		files[i] = FileInfo{
			Name:    e.Name(),
			Size:    e.Size(),
			IsDir:   e.IsDir(),
			ModTime: e.ModTime(),
			Mode:    e.Mode(),
		}
	}
	return files, nil
}

func (c *Client) Mkdir(path string) error {
	return c.sftpClient.MkdirAll(path)
}

func (c *Client) Remove(path string) error {
	return c.sftpClient.Remove(path)
}

func (c *Client) RemoveDirectory(path string) error {
	return c.sftpClient.RemoveDirectory(path)
}

func (c *Client) Rename(oldPath, newPath string) error {
	return c.sftpClient.Rename(oldPath, newPath)
}

func (c *Client) Chmod(path string, mode os.FileMode) error {
	return c.sftpClient.Chmod(path, mode)
}

func (c *Client) Stat(path string) (*FileInfo, error) {
	info, err := c.sftpClient.Stat(path)
	if err != nil {
		return nil, err
	}
	return &FileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
		Mode:    info.Mode(),
	}, nil
}

func (c *Client) SFTPClient() *sftp.Client {
	return c.sftpClient
}

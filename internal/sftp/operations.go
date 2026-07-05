package sftp

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type TransferProgress struct {
	TotalBytes       int64
	TransferredBytes int64
	CurrentFile      string
	Done             bool
	FileIndex        int // 1-based index of current file in batch
	TotalFiles       int // total files in batch (0 = unknown)
}

type ProgressCallback func(TransferProgress)

type RemoteRemoveFS interface {
	List(path string) ([]FileInfo, error)
	Remove(path string) error
	RemoveDirectory(path string) error
}

func Upload(client *Client, localPath string, remotePath string, progress ProgressCallback) error {
	bs := &batchState{total: 1}
	transferred, err := uploadOne(client, localPath, remotePath, progress, bs)
	if err != nil {
		return err
	}
	if progress != nil {
		progress(TransferProgress{
			TotalBytes:       transferred,
			TransferredBytes: transferred,
			CurrentFile:      filepath.Base(localPath),
			Done:             true,
			FileIndex:        1,
			TotalFiles:       1,
		})
	}
	return nil
}

func Download(client *Client, remotePath string, localPath string, progress ProgressCallback) error {
	bs := &batchState{total: 1}
	transferred, err := downloadOne(client, remotePath, localPath, progress, bs)
	if err != nil {
		return err
	}
	if progress != nil {
		progress(TransferProgress{
			TotalBytes:       transferred,
			TransferredBytes: transferred,
			CurrentFile:      filepath.Base(remotePath),
			Done:             true,
			FileIndex:        1,
			TotalFiles:       1,
		})
	}
	return nil
}

// countLocalFiles recursively counts regular files under dir.
func countLocalFiles(dir string) int {
	n := 0
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.IsDir() {
			n += countLocalFiles(filepath.Join(dir, e.Name()))
		} else {
			n++
		}
	}
	return n
}

// countRemoteFiles recursively counts regular files under dir.
func countRemoteFiles(client *Client, dir string) int {
	n := 0
	entries, err := client.List(dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.IsDir {
			n += countRemoteFiles(client, dir+"/"+e.Name)
		} else {
			n++
		}
	}
	return n
}

// batchState tracks file index across a recursive dir transfer.
type batchState struct {
	index int
	total int
}

func uploadOne(client *Client, localPath, remotePath string, progress ProgressCallback, bs *batchState) (int64, error) {
	localFile, err := os.Open(localPath)
	if err != nil {
		return 0, fmt.Errorf("failed to open local file: %w", err)
	}
	defer localFile.Close()

	stat, err := localFile.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to stat local file: %w", err)
	}

	remoteFile, err := client.sftpClient.Create(remotePath)
	if err != nil {
		return 0, fmt.Errorf("failed to create remote file: %w", err)
	}
	defer remoteFile.Close()

	bs.index++
	totalBytes := stat.Size()
	var transferred int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := localFile.Read(buf)
		if n > 0 {
			_, writeErr := remoteFile.Write(buf[:n])
			if writeErr != nil {
				return 0, fmt.Errorf("failed to write remote file: %w", writeErr)
			}
			transferred += int64(n)
			if progress != nil {
				progress(TransferProgress{
					TotalBytes:       totalBytes,
					TransferredBytes: transferred,
					CurrentFile:      filepath.Base(localPath),
					FileIndex:        bs.index,
					TotalFiles:       bs.total,
				})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return 0, fmt.Errorf("failed to read local file: %w", readErr)
		}
	}
	return transferred, nil
}

func uploadDirRecursive(client *Client, localDir, remoteDir string, progress ProgressCallback, bs *batchState) error {
	err := client.Mkdir(remoteDir)
	if err != nil {
		return fmt.Errorf("failed to create remote directory: %w", err)
	}
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return fmt.Errorf("failed to read local directory: %w", err)
	}
	for _, entry := range entries {
		lp := filepath.Join(localDir, entry.Name())
		rp := remoteDir + "/" + entry.Name()
		if entry.IsDir() {
			if err := uploadDirRecursive(client, lp, rp, progress, bs); err != nil {
				return err
			}
		} else {
			if _, err := uploadOne(client, lp, rp, progress, bs); err != nil {
				return err
			}
		}
	}
	return nil
}

func UploadDir(client *Client, localDir string, remoteDir string, progress ProgressCallback) error {
	total := countLocalFiles(localDir)
	bs := &batchState{total: total}
	err := uploadDirRecursive(client, localDir, remoteDir, progress, bs)
	if err != nil {
		return err
	}
	if progress != nil {
		progress(TransferProgress{Done: true, FileIndex: bs.index, TotalFiles: bs.total})
	}
	return nil
}

func downloadOne(client *Client, remotePath, localPath string, progress ProgressCallback, bs *batchState) (int64, error) {
	remoteFile, err := client.sftpClient.Open(remotePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open remote file: %w", err)
	}
	defer remoteFile.Close()

	stat, err := remoteFile.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to stat remote file: %w", err)
	}

	localFile, err := os.Create(localPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create local file: %w", err)
	}
	defer localFile.Close()

	bs.index++
	totalBytes := stat.Size()
	var transferred int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := remoteFile.Read(buf)
		if n > 0 {
			_, writeErr := localFile.Write(buf[:n])
			if writeErr != nil {
				return 0, fmt.Errorf("failed to write local file: %w", writeErr)
			}
			transferred += int64(n)
			if progress != nil {
				progress(TransferProgress{
					TotalBytes:       totalBytes,
					TransferredBytes: transferred,
					CurrentFile:      filepath.Base(remotePath),
					FileIndex:        bs.index,
					TotalFiles:       bs.total,
				})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return 0, fmt.Errorf("failed to read remote file: %w", readErr)
		}
	}
	return transferred, nil
}

func downloadDirRecursive(client *Client, remoteDir, localDir string, progress ProgressCallback, bs *batchState) error {
	err := os.MkdirAll(localDir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create local directory: %w", err)
	}
	entries, err := client.List(remoteDir)
	if err != nil {
		return fmt.Errorf("failed to list remote directory: %w", err)
	}
	for _, entry := range entries {
		rp := remoteDir + "/" + entry.Name
		lp := filepath.Join(localDir, entry.Name)
		if entry.IsDir {
			if err := downloadDirRecursive(client, rp, lp, progress, bs); err != nil {
				return err
			}
		} else {
			if _, err := downloadOne(client, rp, lp, progress, bs); err != nil {
				return err
			}
		}
	}
	return nil
}

func DownloadDir(client *Client, remoteDir string, localDir string, progress ProgressCallback) error {
	total := countRemoteFiles(client, remoteDir)
	bs := &batchState{total: total}
	err := downloadDirRecursive(client, remoteDir, localDir, progress, bs)
	if err != nil {
		return err
	}
	if progress != nil {
		progress(TransferProgress{Done: true, FileIndex: bs.index, TotalFiles: bs.total})
	}
	return nil
}

func RemoveRemoteAll(fs RemoteRemoveFS, remotePath string) error {
	entries, err := fs.List(remotePath)
	if err != nil {
		return fs.Remove(remotePath)
	}
	for _, entry := range entries {
		child := remoteChild(remotePath, entry.Name)
		if entry.IsDir {
			if err := RemoveRemoteAll(fs, child); err != nil {
				return err
			}
		} else if err := fs.Remove(child); err != nil {
			return err
		}
	}
	return fs.RemoveDirectory(remotePath)
}

func remoteChild(dir, name string) string {
	if dir == "/" {
		return "/" + strings.TrimLeft(name, "/")
	}
	return strings.TrimRight(dir, "/") + "/" + strings.TrimLeft(name, "/")
}

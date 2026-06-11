package sftp

import (
	"reflect"
	"testing"
	"time"
)

type fakeRemoteFS struct {
	files   map[string][]FileInfo
	events  []string
	failMap map[string]error
}

func (f *fakeRemoteFS) List(path string) ([]FileInfo, error) {
	if err := f.failMap["list:"+path]; err != nil {
		return nil, err
	}
	return f.files[path], nil
}

func (f *fakeRemoteFS) Remove(path string) error {
	if err := f.failMap["remove:"+path]; err != nil {
		return err
	}
	f.events = append(f.events, "file:"+path)
	return nil
}

func (f *fakeRemoteFS) RemoveDirectory(path string) error {
	if err := f.failMap["rmdir:"+path]; err != nil {
		return err
	}
	f.events = append(f.events, "dir:"+path)
	return nil
}

func TestRemoveRemoteAllDeletesChildrenBeforeDirectory(t *testing.T) {
	fs := &fakeRemoteFS{
		files: map[string][]FileInfo{
			"/root": {
				{Name: "a.txt"},
				{Name: "sub", IsDir: true},
			},
			"/root/sub": {
				{Name: "b.txt", ModTime: time.Now()},
			},
		},
		failMap: map[string]error{},
	}

	err := RemoveRemoteAll(fs, "/root")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"file:/root/a.txt",
		"file:/root/sub/b.txt",
		"dir:/root/sub",
		"dir:/root",
	}
	if !reflect.DeepEqual(fs.events, want) {
		t.Fatalf("got %#v want %#v", fs.events, want)
	}
}

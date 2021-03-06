// Code generated by "esc "; DO NOT EDIT.

package js

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"sync"
	"time"
)

type _escLocalFS struct{}

var _escLocal _escLocalFS

type _escStaticFS struct{}

var _escStatic _escStaticFS

type _escDirectory struct {
	fs   http.FileSystem
	name string
}

type _escFile struct {
	compressed string
	size       int64
	modtime    int64
	local      string
	isDir      bool

	once sync.Once
	data []byte
	name string
}

func (_escLocalFS) Open(name string) (http.File, error) {
	f, present := _escData[path.Clean(name)]
	if !present {
		return nil, os.ErrNotExist
	}
	return os.Open(f.local)
}

func (_escStaticFS) prepare(name string) (*_escFile, error) {
	f, present := _escData[path.Clean(name)]
	if !present {
		return nil, os.ErrNotExist
	}
	var err error
	f.once.Do(func() {
		f.name = path.Base(name)
		if f.size == 0 {
			return
		}
		var gr *gzip.Reader
		b64 := base64.NewDecoder(base64.StdEncoding, bytes.NewBufferString(f.compressed))
		gr, err = gzip.NewReader(b64)
		if err != nil {
			return
		}
		f.data, err = ioutil.ReadAll(gr)
	})
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (fs _escStaticFS) Open(name string) (http.File, error) {
	f, err := fs.prepare(name)
	if err != nil {
		return nil, err
	}
	return f.File()
}

func (dir _escDirectory) Open(name string) (http.File, error) {
	return dir.fs.Open(dir.name + name)
}

func (f *_escFile) File() (http.File, error) {
	type httpFile struct {
		*bytes.Reader
		*_escFile
	}
	return &httpFile{
		Reader:   bytes.NewReader(f.data),
		_escFile: f,
	}, nil
}

func (f *_escFile) Close() error {
	return nil
}

func (f *_escFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, nil
}

func (f *_escFile) Stat() (os.FileInfo, error) {
	return f, nil
}

func (f *_escFile) Name() string {
	return f.name
}

func (f *_escFile) Size() int64 {
	return f.size
}

func (f *_escFile) Mode() os.FileMode {
	return 0
}

func (f *_escFile) ModTime() time.Time {
	return time.Unix(f.modtime, 0)
}

func (f *_escFile) IsDir() bool {
	return f.isDir
}

func (f *_escFile) Sys() interface{} {
	return f
}

// _escFS returns a http.Filesystem for the embedded assets. If useLocal is true,
// the filesystem's contents are instead used.
func _escFS(useLocal bool) http.FileSystem {
	if useLocal {
		return _escLocal
	}
	return _escStatic
}

// _escDir returns a http.Filesystem for the embedded assets on a given prefix dir.
// If useLocal is true, the filesystem's contents are instead used.
func _escDir(useLocal bool, name string) http.FileSystem {
	if useLocal {
		return _escDirectory{fs: _escLocal, name: name}
	}
	return _escDirectory{fs: _escStatic, name: name}
}

// _escFSByte returns the named file from the embedded assets. If useLocal is
// true, the filesystem's contents are instead used.
func _escFSByte(useLocal bool, name string) ([]byte, error) {
	if useLocal {
		f, err := _escLocal.Open(name)
		if err != nil {
			return nil, err
		}
		b, err := ioutil.ReadAll(f)
		_ = f.Close()
		return b, err
	}
	f, err := _escStatic.prepare(name)
	if err != nil {
		return nil, err
	}
	return f.data, nil
}

// _escFSMustByte is the same as _escFSByte, but panics if name is not present.
func _escFSMustByte(useLocal bool, name string) []byte {
	b, err := _escFSByte(useLocal, name)
	if err != nil {
		panic(err)
	}
	return b
}

// _escFSString is the string version of _escFSByte.
func _escFSString(useLocal bool, name string) (string, error) {
	b, err := _escFSByte(useLocal, name)
	return string(b), err
}

// _escFSMustString is the string version of _escFSMustByte.
func _escFSMustString(useLocal bool, name string) string {
	return string(_escFSMustByte(useLocal, name))
}

var _escData = map[string]*_escFile{

	"/helpers.js": {
		local:   "pkg/js/helpers.js",
		size:    18525,
		modtime: 0,
		compressed: `
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
BLLUrwJ1j3MjweL/BgAA//+VzTYAXUgAAA==
`,
	},

	"/": {
		isDir: true,
		local: "pkg/js",
	},
}

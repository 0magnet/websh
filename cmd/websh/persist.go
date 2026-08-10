//go:build js && wasm

package main

// IndexedDB persistence for the in-memory filesystem. afero's
// interface is synchronous while IndexedDB is async-only, so instead
// of backing afero with IndexedDB directly, the MemMapFs stays the
// working filesystem and is write-through persisted: restored from
// IndexedDB at startup, and diffed+flushed after every command.

import (
	"errors"
	"os"
	"syscall/js"
	"time"

	"github.com/0magnet/afero"
)

const (
	idbName  = "websh-fs"
	idbStore = "files"
)

// awaitReq blocks the calling goroutine until an IDBRequest settles.
func awaitReq(req js.Value) (js.Value, error) {
	done := make(chan error, 1)
	var result js.Value
	onOK := js.FuncOf(func(js.Value, []js.Value) any {
		result = req.Get("result")
		done <- nil
		return nil
	})
	onErr := js.FuncOf(func(js.Value, []js.Value) any {
		done <- errors.New("indexeddb request failed")
		return nil
	})
	defer onOK.Release()
	defer onErr.Release()
	req.Set("onsuccess", onOK)
	req.Set("onerror", onErr)
	err := <-done
	return result, err
}

type idbStorage struct {
	db js.Value
}

// openStorage opens (and if needed creates) the database.
func openStorage() (*idbStorage, error) {
	idb := js.Global().Get("indexedDB")
	if idb.IsUndefined() {
		return nil, errors.New("indexedDB unavailable")
	}
	req := idb.Call("open", idbName, 1)
	upgrade := js.FuncOf(func(_ js.Value, args []js.Value) any {
		db := req.Get("result")
		if !db.Get("objectStoreNames").Call("contains", idbStore).Bool() {
			db.Call("createObjectStore", idbStore)
		}
		return nil
	})
	defer upgrade.Release()
	req.Set("onupgradeneeded", upgrade)
	db, err := awaitReq(req)
	if err != nil {
		return nil, err
	}
	return &idbStorage{db: db}, nil
}

func (s *idbStorage) store(mode string) js.Value {
	return s.db.Call("transaction", idbStore, mode).Call("objectStore", idbStore)
}

// fileRecord is what gets stored per path.
type fileRecord struct {
	isDir bool
	mode  os.FileMode
	mtime time.Time
	data  []byte
}

func (s *idbStorage) put(path string, rec fileRecord) error {
	val := js.Global().Get("Object").New()
	val.Set("isDir", rec.isDir)
	val.Set("mode", uint32(rec.mode))
	val.Set("mtime", rec.mtime.UnixMilli())
	if !rec.isDir {
		arr := js.Global().Get("Uint8Array").New(len(rec.data))
		js.CopyBytesToJS(arr, rec.data)
		val.Set("data", arr)
	}
	_, err := awaitReq(s.store("readwrite").Call("put", val, path))
	return err
}

func (s *idbStorage) delete(path string) error {
	_, err := awaitReq(s.store("readwrite").Call("delete", path))
	return err
}

func (s *idbStorage) clear() error {
	_, err := awaitReq(s.store("readwrite").Call("clear"))
	return err
}

// loadAll restores every stored path into the filesystem. Returns the
// number of restored entries (0 = fresh database).
func (s *idbStorage) loadAll(vfs afero.Fs) (int, error) {
	st := s.store("readonly")
	keys, err := awaitReq(st.Call("getAllKeys"))
	if err != nil {
		return 0, err
	}
	vals, err := awaitReq(st.Call("getAll"))
	if err != nil {
		return 0, err
	}
	n := keys.Get("length").Int()
	for i := 0; i < n; i++ {
		path := keys.Index(i).String()
		val := vals.Index(i)
		mode := os.FileMode(val.Get("mode").Int())
		mtime := time.UnixMilli(int64(val.Get("mtime").Float()))
		if val.Get("isDir").Bool() {
			_ = vfs.MkdirAll(path, mode.Perm())
			continue
		}
		arr := val.Get("data")
		data := make([]byte, arr.Get("length").Int())
		js.CopyBytesToGo(data, arr)
		_ = afero.WriteFile(vfs, path, data, mode.Perm())
		_ = vfs.Chtimes(path, mtime, mtime)
	}
	return n, nil
}

// fileSig detects changes cheaply between syncs.
type fileSig struct {
	isDir bool
	size  int64
	mtime int64
}

// syncFS walks the filesystem, persists new/changed entries, removes
// deleted ones, and returns the new signature snapshot.
func (s *idbStorage) syncFS(vfs afero.Fs, prev map[string]fileSig) map[string]fileSig {
	next := make(map[string]fileSig, len(prev))
	_ = afero.Walk(vfs, "/", func(path string, info os.FileInfo, err error) error {
		if err != nil || path == "/" {
			return nil
		}
		sig := fileSig{isDir: info.IsDir(), size: info.Size(), mtime: info.ModTime().UnixMilli()}
		next[path] = sig
		if old, ok := prev[path]; ok && old == sig {
			return nil
		}
		rec := fileRecord{isDir: info.IsDir(), mode: info.Mode(), mtime: info.ModTime()}
		if !info.IsDir() {
			data, err := afero.ReadFile(vfs, path)
			if err != nil {
				return nil
			}
			rec.data = data
		}
		_ = s.put(path, rec)
		return nil
	})
	for path := range prev {
		if _, ok := next[path]; !ok {
			_ = s.delete(path)
		}
	}
	return next
}

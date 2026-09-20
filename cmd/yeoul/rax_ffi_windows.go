//go:build windows

package main

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// raxWindowsLib resolves every required rax FFI export before any operation
// runs. syscall.LazyProc.Call panics when a library or export is missing, which
// would bypass the ordinary core fallback in auto mode, so the DLL and its
// procedures are resolved up front and reported as errors instead.
type raxWindowsLib struct {
	dll                *syscall.DLL
	ingestDocs         *syscall.Proc
	search             *syscall.Proc
	searchDocIDs       *syscall.Proc
	openReadOnly       *syscall.Proc
	handleSearchDocIDs *syscall.Proc
	handleClose        *syscall.Proc
	stringFree         *syscall.Proc
	lastError          *syscall.Proc
}

func openRaxWindowsLib(libPath string) (*raxWindowsLib, error) {
	if _, err := syscall.BytePtrFromString(libPath); err != nil {
		return nil, fmt.Errorf("rax ffi: invalid library path %q: %w", libPath, err)
	}
	dll, err := syscall.LoadDLL(libPath)
	if err != nil {
		return nil, fmt.Errorf("rax ffi: load %q: %w", libPath, err)
	}
	lib := &raxWindowsLib{dll: dll}
	required := func(name string) (*syscall.Proc, error) {
		proc, err := dll.FindProc(name)
		if err != nil {
			return nil, fmt.Errorf("rax ffi: resolve %s in %q: %w", name, libPath, err)
		}
		return proc, nil
	}
	optional := func(name string) *syscall.Proc {
		proc, err := dll.FindProc(name)
		if err != nil {
			return nil
		}
		return proc
	}
	if lib.ingestDocs, err = required("rax_ingest_docs"); err != nil {
		lib.close()
		return nil, err
	}
	if lib.search, err = required("rax_search"); err != nil {
		lib.close()
		return nil, err
	}
	if lib.stringFree, err = required("rax_string_free"); err != nil {
		lib.close()
		return nil, err
	}
	if lib.lastError, err = required("rax_last_error"); err != nil {
		lib.close()
		return nil, err
	}
	lib.searchDocIDs = optional("rax_search_doc_ids")
	lib.openReadOnly = optional("rax_open_read_only")
	lib.handleSearchDocIDs = optional("rax_handle_search_doc_ids")
	lib.handleClose = optional("rax_handle_close")
	return lib, nil
}

func (lib *raxWindowsLib) close() {
	if lib != nil && lib.dll != nil {
		_ = lib.dll.Release()
		lib.dll = nil
	}
}

func raxFFIIngestDocs(libPath, storePath string, jsonl []byte) ([]byte, error) {
	lib, err := openRaxWindowsLib(libPath)
	if err != nil {
		return nil, err
	}
	defer lib.close()
	return lib.ingestDocsJSONL(storePath, jsonl)
}

func (lib *raxWindowsLib) ingestDocsJSONL(storePath string, jsonl []byte) ([]byte, error) {
	// Rax stores errors thread-locally, so the error read below must stay on the
	// thread that ran the failing operation.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	store, err := syscall.BytePtrFromString(storePath)
	if err != nil {
		return nil, err
	}
	var out *byte
	var dataPtr uintptr
	if len(jsonl) > 0 {
		dataPtr = uintptr(unsafe.Pointer(&jsonl[0]))
	}
	status, _, callErr := lib.ingestDocs.Call(uintptr(unsafe.Pointer(store)), dataPtr, uintptr(len(jsonl)), uintptr(unsafe.Pointer(&out)))
	return lib.output(status, callErr, out)
}

func raxFFISearchText(libPath, storePath, query string, topK int) ([]byte, error) {
	lib, err := openRaxWindowsLib(libPath)
	if err != nil {
		return nil, err
	}
	defer lib.close()
	return lib.searchText(storePath, query, topK)
}

func (lib *raxWindowsLib) searchText(storePath, query string, topK int) ([]byte, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	store, err := syscall.BytePtrFromString(storePath)
	if err != nil {
		return nil, err
	}
	mode, _ := syscall.BytePtrFromString("text")
	text, err := syscall.BytePtrFromString(query)
	if err != nil {
		return nil, err
	}
	var out *byte
	if lib.searchDocIDs != nil {
		status, _, callErr := lib.searchDocIDs.Call(
			uintptr(unsafe.Pointer(store)),
			uintptr(unsafe.Pointer(mode)),
			uintptr(unsafe.Pointer(text)),
			0,
			uintptr(topK),
			uintptr(unsafe.Pointer(&out)),
		)
		return lib.output(status, callErr, out)
	}
	status, _, callErr := lib.search.Call(
		uintptr(unsafe.Pointer(store)),
		uintptr(unsafe.Pointer(mode)),
		uintptr(unsafe.Pointer(text)),
		0,
		uintptr(topK),
		0,
		uintptr(unsafe.Pointer(&out)),
	)
	return lib.output(status, callErr, out)
}

type raxFFISearcher struct {
	lib    *raxWindowsLib
	handle unsafe.Pointer
}

func openRaxFFISearcher(libPath, storePath string) (*raxFFISearcher, error) {
	lib, err := openRaxWindowsLib(libPath)
	if err != nil {
		return nil, err
	}
	searcher := &raxFFISearcher{lib: lib}
	if lib.openReadOnly != nil && lib.handleSearchDocIDs != nil && lib.handleClose != nil {
		store, err := syscall.BytePtrFromString(storePath)
		if err != nil {
			lib.close()
			return nil, err
		}
		var handle unsafe.Pointer
		// Rax reports failures through thread-local state, so the open call and the
		// error extraction that follows it must run on the same OS thread.
		runtime.LockOSThread()
		status, _, callErr := lib.openReadOnly.Call(uintptr(unsafe.Pointer(store)), uintptr(unsafe.Pointer(&handle)))
		if status != 0 {
			_, err := lib.output(status, callErr, nil)
			runtime.UnlockOSThread()
			lib.close()
			return nil, err
		}
		runtime.UnlockOSThread()
		searcher.handle = handle
	}
	return searcher, nil
}

func (s *raxFFISearcher) close() {
	if s == nil || s.lib == nil {
		return
	}
	if s.handle != nil && s.lib.handleClose != nil {
		s.lib.handleClose.Call(uintptr(s.handle))
		s.handle = nil
	}
	s.lib.close()
	s.lib = nil
}

func (s *raxFFISearcher) searchText(storePath, query string, topK int) ([]byte, error) {
	if s != nil && s.handle != nil {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		mode, _ := syscall.BytePtrFromString("text")
		text, err := syscall.BytePtrFromString(query)
		if err != nil {
			return nil, err
		}
		var out *byte
		status, _, callErr := s.lib.handleSearchDocIDs.Call(
			uintptr(s.handle),
			uintptr(unsafe.Pointer(mode)),
			uintptr(unsafe.Pointer(text)),
			0,
			uintptr(topK),
			uintptr(unsafe.Pointer(&out)),
		)
		return s.lib.output(status, callErr, out)
	}
	if s == nil || s.lib == nil {
		return nil, errors.New("rax ffi: searcher is closed")
	}
	return s.lib.searchText(storePath, query, topK)
}

func (lib *raxWindowsLib) output(status uintptr, callErr error, out *byte) ([]byte, error) {
	if status != 0 {
		if msgPtr, _, _ := lib.lastError.Call(); msgPtr != 0 {
			return nil, errors.New(windowsBytePtrString((*byte)(unsafe.Pointer(msgPtr))))
		}
		if callErr != nil {
			return nil, callErr
		}
		return nil, errors.New("rax ffi: operation failed")
	}
	if out == nil {
		return nil, nil
	}
	defer lib.stringFree.Call(uintptr(unsafe.Pointer(out)))
	return []byte(windowsBytePtrString(out)), nil
}

func windowsBytePtrString(ptr *byte) string {
	if ptr == nil {
		return ""
	}
	var bytes []byte
	for p := uintptr(unsafe.Pointer(ptr)); ; p++ {
		b := *(*byte)(unsafe.Pointer(p))
		if b == 0 {
			return string(bytes)
		}
		bytes = append(bytes, b)
	}
}

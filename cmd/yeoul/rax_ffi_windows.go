//go:build windows

package main

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

func raxFFIIngestDocs(libPath, storePath string, jsonl []byte) ([]byte, error) {
	dll := syscall.NewLazyDLL(libPath)
	ingest := dll.NewProc("rax_ingest_docs")
	free := dll.NewProc("rax_string_free")
	lastError := dll.NewProc("rax_last_error")
	store, err := syscall.BytePtrFromString(storePath)
	if err != nil {
		return nil, err
	}
	var out *byte
	var dataPtr uintptr
	if len(jsonl) > 0 {
		dataPtr = uintptr(unsafe.Pointer(&jsonl[0]))
	}
	status, _, callErr := ingest.Call(uintptr(unsafe.Pointer(store)), dataPtr, uintptr(len(jsonl)), uintptr(unsafe.Pointer(&out)))
	return raxWindowsOutput(status, callErr, out, free, lastError)
}

func raxFFISearchText(libPath, storePath, query string, topK int) ([]byte, error) {
	dll := syscall.NewLazyDLL(libPath)
	search := dll.NewProc("rax_search")
	free := dll.NewProc("rax_string_free")
	lastError := dll.NewProc("rax_last_error")
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
	status, _, callErr := search.Call(
		uintptr(unsafe.Pointer(store)),
		uintptr(unsafe.Pointer(mode)),
		uintptr(unsafe.Pointer(text)),
		0,
		uintptr(topK),
		0,
		uintptr(unsafe.Pointer(&out)),
	)
	return raxWindowsOutput(status, callErr, out, free, lastError)
}

type raxFFISearcher struct {
	libPath string
}

func openRaxFFISearcher(libPath, storePath string) (*raxFFISearcher, error) {
	return &raxFFISearcher{libPath: libPath}, nil
}

func (s *raxFFISearcher) close() {}

func (s *raxFFISearcher) searchText(storePath, query string, topK int) ([]byte, error) {
	return raxFFISearchText(s.libPath, storePath, query, topK)
}

func raxWindowsOutput(status uintptr, callErr error, out *byte, free, lastError *syscall.LazyProc) ([]byte, error) {
	if status != 0 {
		msgPtr, _, _ := lastError.Call()
		if msgPtr != 0 {
			return nil, errors.New(syscall.BytePtrToString((*byte)(unsafe.Pointer(msgPtr))))
		}
		return nil, callErr
	}
	if out == nil {
		return nil, nil
	}
	defer free.Call(uintptr(unsafe.Pointer(out)))
	return []byte(syscall.BytePtrToString(out)), nil
}

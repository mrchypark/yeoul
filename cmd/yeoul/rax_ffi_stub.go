//go:build !windows && !cgo

package main

import "fmt"

func raxFFIIngestDocs(libPath, storePath string, jsonl []byte) ([]byte, error) {
	return nil, fmt.Errorf("rax FFI requires cgo on this platform")
}

func raxFFISearchText(libPath, storePath, query string, topK int) ([]byte, error) {
	return nil, fmt.Errorf("rax FFI requires cgo on this platform")
}

type raxFFISearcher struct{}

func openRaxFFISearcher(libPath, storePath string) (*raxFFISearcher, error) {
	return nil, fmt.Errorf("rax FFI requires cgo on this platform")
}

func (s *raxFFISearcher) close() {}

func (s *raxFFISearcher) searchText(storePath, query string, topK int) ([]byte, error) {
	return nil, fmt.Errorf("rax FFI requires cgo on this platform")
}

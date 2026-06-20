//go:build cgo && (darwin || linux)

package main

/*
#cgo linux LDFLAGS: -ldl
#include <stdbool.h>
#include <stdlib.h>
#include <string.h>
#include <dlfcn.h>

typedef int (*rax_ingest_docs_fn)(const char*, const unsigned char*, size_t, char**);
typedef int (*rax_search_fn)(const char*, const char*, const char*, const char*, int, bool, char**);
typedef int (*rax_search_doc_ids_fn)(const char*, const char*, const char*, const char*, int, char**);
typedef int (*rax_open_read_only_fn)(const char*, void**);
typedef int (*rax_handle_search_doc_ids_fn)(void*, const char*, const char*, const char*, int, char**);
typedef void (*rax_handle_close_fn)(void*);
typedef void (*rax_string_free_fn)(char*);
typedef const char* (*rax_last_error_fn)(void);

static void* yeoul_dlopen(const char* path, char** err) {
	void* handle = dlopen(path, RTLD_NOW | RTLD_LOCAL);
	if (handle == NULL) {
		const char* msg = dlerror();
		*err = strdup(msg == NULL ? "dlopen failed" : msg);
	}
	return handle;
}

static void* yeoul_dlsym(void* handle, const char* name, char** err) {
	dlerror();
	void* symbol = dlsym(handle, name);
	const char* msg = dlerror();
	if (msg != NULL) {
		*err = strdup(msg);
		return NULL;
	}
	return symbol;
}

static int yeoul_rax_ingest_docs(void* fn, const char* store, const unsigned char* jsonl, size_t jsonl_len, char** out_json) {
	return ((rax_ingest_docs_fn)fn)(store, jsonl, jsonl_len, out_json);
}

static int yeoul_rax_search_text(void* fn, const char* store, const char* text, int top_k, char** out_json) {
	return ((rax_search_fn)fn)(store, "text", text, NULL, top_k, false, out_json);
}

static int yeoul_rax_search_doc_ids_text(void* fn, const char* store, const char* text, int top_k, char** out_json) {
	return ((rax_search_doc_ids_fn)fn)(store, "text", text, NULL, top_k, out_json);
}

static int yeoul_rax_open_read_only(void* fn, const char* store, void** out_handle) {
	return ((rax_open_read_only_fn)fn)(store, out_handle);
}

static int yeoul_rax_handle_search_doc_ids_text(void* fn, void* handle, const char* text, int top_k, char** out_json) {
	return ((rax_handle_search_doc_ids_fn)fn)(handle, "text", text, NULL, top_k, out_json);
}

static void yeoul_rax_handle_close(void* fn, void* handle) {
	((rax_handle_close_fn)fn)(handle);
}

static void yeoul_rax_string_free(void* fn, char* value) {
	((rax_string_free_fn)fn)(value);
}

static const char* yeoul_rax_last_error(void* fn) {
	return ((rax_last_error_fn)fn)();
}
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

type raxFFIHandle struct {
	handle             unsafe.Pointer
	ingestDocsBytes    unsafe.Pointer
	search             unsafe.Pointer
	searchDocIDs       unsafe.Pointer
	openReadOnly       unsafe.Pointer
	handleSearchDocIDs unsafe.Pointer
	handleClose        unsafe.Pointer
	stringFree         unsafe.Pointer
	lastError          unsafe.Pointer
}

func openRaxFFI(path string) (*raxFFIHandle, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	var cErr *C.char
	handle := C.yeoul_dlopen(cPath, &cErr)
	if handle == nil {
		return nil, cStringError(cErr)
	}
	api := &raxFFIHandle{handle: handle}
	load := func(name string) (unsafe.Pointer, error) {
		cName := C.CString(name)
		defer C.free(unsafe.Pointer(cName))
		var cErr *C.char
		ptr := C.yeoul_dlsym(handle, cName, &cErr)
		if ptr == nil {
			return nil, cStringError(cErr)
		}
		return ptr, nil
	}
	loadOptional := func(name string) unsafe.Pointer {
		ptr, _ := load(name)
		return ptr
	}
	var err error
	if api.ingestDocsBytes, err = load("rax_ingest_docs"); err != nil {
		api.close()
		return nil, err
	}
	if api.search, err = load("rax_search"); err != nil {
		api.close()
		return nil, err
	}
	api.searchDocIDs = loadOptional("rax_search_doc_ids")
	api.openReadOnly = loadOptional("rax_open_read_only")
	api.handleSearchDocIDs = loadOptional("rax_handle_search_doc_ids")
	api.handleClose = loadOptional("rax_handle_close")
	if api.stringFree, err = load("rax_string_free"); err != nil {
		api.close()
		return nil, err
	}
	if api.lastError, err = load("rax_last_error"); err != nil {
		api.close()
		return nil, err
	}
	return api, nil
}

func (api *raxFFIHandle) close() {
	if api != nil && api.handle != nil {
		C.dlclose(api.handle)
		api.handle = nil
	}
}

func raxFFIIngestDocs(libPath, storePath string, jsonl []byte) ([]byte, error) {
	api, err := openRaxFFI(libPath)
	if err != nil {
		return nil, err
	}
	defer api.close()
	cStore := C.CString(storePath)
	defer C.free(unsafe.Pointer(cStore))
	cJSONL := C.CBytes(jsonl)
	defer C.free(cJSONL)
	var out *C.char
	status := C.yeoul_rax_ingest_docs(api.ingestDocsBytes, cStore, (*C.uchar)(cJSONL), C.size_t(len(jsonl)), &out)
	return api.output(status, out)
}

func raxFFISearchText(libPath, storePath, query string, topK int) ([]byte, error) {
	api, err := openRaxFFI(libPath)
	if err != nil {
		return nil, err
	}
	defer api.close()
	return api.searchText(storePath, query, topK)
}

type raxFFISearcher struct {
	api    *raxFFIHandle
	handle unsafe.Pointer
}

func openRaxFFISearcher(libPath, storePath string) (*raxFFISearcher, error) {
	api, err := openRaxFFI(libPath)
	if err != nil {
		return nil, err
	}
	searcher := &raxFFISearcher{api: api}
	if api.openReadOnly != nil && api.handleSearchDocIDs != nil && api.handleClose != nil {
		cStore := C.CString(storePath)
		defer C.free(unsafe.Pointer(cStore))
		var handle unsafe.Pointer
		status := C.yeoul_rax_open_read_only(api.openReadOnly, cStore, &handle)
		if status != 0 {
			api.close()
			return nil, errors.New(C.GoString(C.yeoul_rax_last_error(api.lastError)))
		}
		searcher.handle = handle
	}
	return searcher, nil
}

func (s *raxFFISearcher) close() {
	if s == nil {
		return
	}
	if s.handle != nil && s.api != nil && s.api.handleClose != nil {
		C.yeoul_rax_handle_close(s.api.handleClose, s.handle)
		s.handle = nil
	}
	if s.api != nil {
		s.api.close()
		s.api = nil
	}
}

func (s *raxFFISearcher) searchText(storePath, query string, topK int) ([]byte, error) {
	if s != nil && s.handle != nil {
		cQuery := C.CString(query)
		defer C.free(unsafe.Pointer(cQuery))
		var out *C.char
		status := C.yeoul_rax_handle_search_doc_ids_text(s.api.handleSearchDocIDs, s.handle, cQuery, C.int(topK), &out)
		return s.api.output(status, out)
	}
	return s.api.searchText(storePath, query, topK)
}

func (api *raxFFIHandle) searchText(storePath, query string, topK int) ([]byte, error) {
	cStore := C.CString(storePath)
	defer C.free(unsafe.Pointer(cStore))
	cQuery := C.CString(query)
	defer C.free(unsafe.Pointer(cQuery))
	var out *C.char
	if api.searchDocIDs != nil {
		status := C.yeoul_rax_search_doc_ids_text(api.searchDocIDs, cStore, cQuery, C.int(topK), &out)
		return api.output(status, out)
	}
	status := C.yeoul_rax_search_text(api.search, cStore, cQuery, C.int(topK), &out)
	return api.output(status, out)
}

func (api *raxFFIHandle) output(status C.int, out *C.char) ([]byte, error) {
	if status != 0 {
		return nil, errors.New(C.GoString(C.yeoul_rax_last_error(api.lastError)))
	}
	if out == nil {
		return nil, nil
	}
	defer C.yeoul_rax_string_free(api.stringFree, out)
	return []byte(C.GoString(out)), nil
}

func cStringError(cErr *C.char) error {
	if cErr == nil {
		return fmt.Errorf("rax ffi load failed")
	}
	defer C.free(unsafe.Pointer(cErr))
	return errors.New(C.GoString(cErr))
}

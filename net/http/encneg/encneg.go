// Copyright (c) 2016 Thomas Broyer. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package encneg provides a file server HTTP handler that supports negotiation
// of content encodings based on existing files (no on-the-fly compression), as
// well as a helper to do on-the-fly compression when needed.
//
// The FileServer detects both Brotli and Gzip (Zopfli?) precompressed files,
// whereas the GetWriter helper only does streaming Gzip compression.
//
// The package does not provide a http.Handler middleware for on-the-fly
// compression because a middleware cannot detect cases where compression would
// be wasteful (such as when http.Error() is used, or any other very small
// responses)
package encneg // import "go.ltgt.net/net/http/encneg"

import (
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

var encodingByExtensionMap = map[string]string{
	".br": "br",
	".gz": "gzip",
}

type fileHandler struct {
	fs http.Handler
}

// FileServer returns a handler that serves HTTP requests
// with the contents of the file system rooted at root,
// negotiating the content encoding based on whether a
// precompressed variant of the requested file exists.
//
// To use the operating system's file system implementation,
// use http.Dir:
//
//     http.Handle("/", encneg.FileServer(http.Dir("/tmp")))
//
// As a special case, the returned file server redirects any request
// ending in "/index.html" to the same path, without the final
// "index.html"; just like the standard http.FileServer.
func FileServer(root http.FileSystem) http.Handler {
	return &fileHandler{fs: http.FileServer(root)}
}

func (f *fileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	// Special-case /index.html: let http.FileServer do its redirect
	if strings.HasSuffix(p, "/index.html") {
		f.fs.ServeHTTP(w, r)
		return
	}
	// Directly asked for a compressed file, set correct Content-* headers
	ext := filepath.Ext(p)
	if encoding := encodingByExtensionMap[ext]; encoding != "" {
		f.serveCompressedFile(ext, encoding, r.URL.Path[:len(r.URL.Path)-len(ext)], false, w, r)
		return
	}

	// Try variants successively, based on Accept-Encoding,
	// prefering Brotli to Gzip.
	if strings.HasSuffix(p, "/") {
		p += "index.html"
	}
	ae := r.Header.Get("Accept-Encoding")
	if hasToken(ae, "br") && f.tryServeCompressedFile(".br", "br", p, w, r) {
		return
	} else if hasToken(ae, "gzip") && f.tryServeCompressedFile(".gz", "gzip", p, w, r) {
		return
	}
	// Note that this unconditionally sends a "Vary: Accept-Encoding" response
	// header, whether there actually exist variants or not, because the cost
	// of checking for a variant would outweight the implications of the Vary
	// header (namely that intermediary caches will have to store one response
	// per Accept-Encoding request header value).
	f.fs.ServeHTTP(&responseWithContentEncoding{w: w, isConneg: true}, r)
}

func (f *fileHandler) tryServeCompressedFile(ext, encoding, path string, w http.ResponseWriter, r *http.Request) bool {
	oldPath := r.URL.Path
	r.URL.Path = path + ext
	crw := &connegResponseWriter{realWriter: w}
	f.serveCompressedFile(ext, encoding, path, true, crw, r)
	r.URL.Path = oldPath
	return !crw.Suppressed
}

func (f *fileHandler) serveCompressedFile(ext, encoding, path string, isConneg bool, w http.ResponseWriter, r *http.Request) {
	// Set Content-Type proactively to bypass content sniffing
	// (it'll be overridden in case of redirect or error anyway),
	// but only set Content-Encoding on success (as it wouldn't
	// be reset otherwise, and suppresses Content-Length; see
	// https://golang.org/issue/1905).
	// If we can't determine Content-Type though, then let the
	// http.FileServer respond with its detected Content-Type
	// (most likely application/gzip, or application/x-gzip for
	// gzipped content).
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		w.Header().Set("Content-Type", ct)
		w = &responseWithContentEncoding{w: w, encoding: encoding, isConneg: isConneg}
	}
	f.fs.ServeHTTP(w, r)
}

func hasToken(header, token string) bool {
	// Note: this is an approximation;
	// It notably does not respect qvalues, and is not case-insensitive.
	// In practice, major browsers do not send qvalues and use lowercase.
	i := strings.Index(header, token)
	return i >= 0 &&
		(i == 0 || isSeparator(header[i-1])) &&
		(i+len(token) == len(header) || isSeparator(header[i+len(token)]))
}

func isSeparator(b byte) bool {
	return strings.IndexByte(" \t;,", b) >= 0
}

// A connegResponseWriter is an http.ResponseWriter that buffers headers until
// WriteHeader (or Write) is called.
//
// When WriteHeader is called with an http.StatusNotFound status code, then
// those headers are dropped and all subsequent calls to Write are no-ops,
// and Suppressed is set to true. In all other cases, the buffered headers are
// copied to the realWriter and subsequent calls to Write pass through to
// the realWriter too.
type connegResponseWriter struct {
	Suppressed  bool
	realWriter  http.ResponseWriter
	header      http.Header
	wroteHeader bool
}

func (w *connegResponseWriter) Header() http.Header {
	if w.wroteHeader && !w.Suppressed {
		return w.realWriter.Header()
	}
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *connegResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	if code == http.StatusNotFound {
		w.Suppressed = true
		return
	}
	for k, v := range w.header {
		h := w.realWriter.Header()
		h[k] = append(h[k], v...)
	}
	w.header = nil
	w.realWriter.WriteHeader(code)
}

func (w *connegResponseWriter) Write(b []byte) (int, error) {
	if w.Suppressed {
		return len(b), nil
	}
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.realWriter.Write(b)
}

// ReadFrom is here to optimize copying from an *os.File regular file,
// because we know the default http.ResponseWriter is an io.ReaderFrom
// and http.FileServer takes advantage of it.
func (w *connegResponseWriter) ReadFrom(src io.Reader) (n int64, err error) {
	if w.Suppressed {
		return 0, nil
	}
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return io.Copy(w.realWriter, src)
}

// A responseWithContentEncoding is an http.ResponseWriter that automatically
// adds a Content-Encoding and/or a "Vary: Accept-Encoding" response header
// whenever WriteHeader is called with an http.StatusOK status code (or Write
// is called without a prior call to WriteHeader).
type responseWithContentEncoding struct {
	w        http.ResponseWriter
	encoding string
	isConneg bool

	headersSent bool
}

func (r *responseWithContentEncoding) Header() http.Header {
	return r.w.Header()
}

func (r *responseWithContentEncoding) WriteHeader(code int) {
	if r.headersSent {
		return
	}
	r.headersSent = true
	if code == http.StatusOK {
		if r.encoding != "" {
			r.Header().Set("Content-Encoding", r.encoding)
		}
		if r.isConneg {
			r.Header().Set("Vary", "Accept-Encoding")
		}
	}
	r.w.WriteHeader(code)
}

func (r *responseWithContentEncoding) Write(b []byte) (int, error) {
	if !r.headersSent {
		r.WriteHeader(http.StatusOK)
	}
	return r.w.Write(b)
}

// ReadFrom is here to optimize copying from an *os.File regular file,
// because we know the default http.ResponseWriter is an io.ReaderFrom
// and http.FileServer takes advantage of it.
func (r *responseWithContentEncoding) ReadFrom(src io.Reader) (n int64, err error) {
	if !r.headersSent {
		r.WriteHeader(http.StatusOK)
	}
	return io.Copy(r.w, src)
}

// GetWriter negotiates whether compression should be used and returns an
// appropriate io.Writer. The returned writer may implement io.Closer, in which
// case it is the caller's responsibility to Close it.
//
// Typical use is of the form:
//	gw := encneg.GetWriter(w, r)
//	if c, ok := gw.(io.Closer); ok {
//		defer c.Close()
//	}
// 	// ...
func GetWriter(w http.ResponseWriter, r *http.Request) io.Writer {
	w.Header().Add("Vary", "Accept-Encoding")
	if hasToken(r.Header.Get("Accept-Encoding"), "gzip") {
		return gzip.NewWriter(w)
	}
	return w
}

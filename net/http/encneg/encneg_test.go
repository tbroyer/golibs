// Copyright (c) 2016 Thomas Broyer. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package encneg

import (
	"compress/gzip"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/tools/godoc/vfs/httpfs"
	"golang.org/x/tools/godoc/vfs/mapfs"
)

var fsmap = map[string]string{
	"uncompressed/index.html":      "index, uncompressed, no alternative",
	"uncompressed/foo.html":        "foo, uncompressed, no alternative",
	"with.br/index.html":           "index, uncompressed, with brotli alternative",
	"with.br/index.html.br":        "index, brotli, with uncompressed alternative",
	"with.br/foo.html":             "foo, uncompressed, with brotli alternative",
	"with.br/foo.html.br":          "foo, brotli, with uncompressed alternative",
	"with.gz/index.html":           "index, uncompressed, with gzip alternative",
	"with.gz/index.html.gz":        "index, gzip, with uncompressed alternative",
	"with.gz/foo.html":             "foo, uncompressed, with gzip alternative",
	"with.gz/foo.html.gz":          "foo, gzip, with uncompressed alternative",
	"with.br.and.gz/index.html":    "index, uncompressed, with gzip and brotli alternatives",
	"with.br.and.gz/index.html.br": "index, brotli, with uncompressed and gzip alternatives",
	"with.br.and.gz/index.html.gz": "index, gzip, with uncompressed and brotli alternatives",
	"with.br.and.gz/foo.html":      "foo, uncompressed, with gzip and brotli alternatives",
	"with.br.and.gz/foo.html.br":   "foo, brotli, with uncompressed and gzip alternatives",
	"with.br.and.gz/foo.html.gz":   "foo, gzip, with uncompressed and brotli alternatives",
}
var fs = FileServer(httpfs.New(mapfs.New(fsmap)))

type ae struct {
	ae            string
	expectsGzip   bool
	expectsBrotli bool
}

var aes = []ae{
	{"", false, false},
	{"br", false, true},
	{"gzip", true, false},
	{"br,gzip", true, true},
	{"gzip,br", true, true},
}

func TestHasToken(t *testing.T) {
	tests := []ae{
		{"foo,gzip,bar,br,baz", true, true},
		{"foogzip", false, false},
		{"gzipbar", false, false},
		{"foogzipbar", false, false},
		{"foobr", false, false},
		{"braz", false, false},
		{"foobraz", false, false},
	}
	for _, ae := range append(tests, aes...) {
		if g, e := hasToken(ae.ae, "gzip"), ae.expectsGzip; g != e {
			t.Errorf("test %q: gzip = %t, want %t", ae.ae, g, e)
		}
		if g, e := hasToken(ae.ae, "br"), ae.expectsBrotli; g != e {
			t.Errorf("test %q: br = %t, want %t", ae.ae, g, e)
		}
	}
}

type testInput struct {
	dir    string
	suffix string
	ae     ae
}

func (t *testInput) String() string {
	return t.dir + t.suffix + "[" + t.ae.ae + "]"
}

func getTests(suffixes []string) (t []testInput) {
	for _, dir := range []string{"/uncompressed", "/with.br", "/with.gz", "/with.br.and.gz"} {
		for _, ae := range aes {
			for _, suffix := range suffixes {
				t = append(t, testInput{dir, suffix, ae})
			}
		}
	}
	return
}

func TestFileServerDirectoryRedirects(t *testing.T) {
	for _, tt := range getTests([]string{"", "/index.html"}) {
		doTest(t, testData{
			path:     tt.dir + tt.suffix,
			wantCode: http.StatusMovedPermanently,
			wantBody: "",
		})
	}
}

func TestFileServerDirectCompressedFiles(t *testing.T) {
	for _, tt := range getTests([]string{"/index.html", "/foo.html"}) {
		for ext, encoding := range encodingByExtensionMap {
			path := tt.dir + tt.suffix + ext
			fpath := path[1:]
			body, hasBody := fsmap[fpath]
			code, ct, ce := http.StatusOK, "text/html; charset=utf-8", encoding
			if !hasBody {
				code, ce, body = http.StatusNotFound, "", "404 page not found\n"
				ct = "text/plain; charset=utf-8" // from http.Error()
			}

			doTest(t, testData{
				path:                path,
				acceptEncoding:      tt.ae.ae,
				wantCode:            code,
				wantContentType:     ct,
				wantContentEncoding: ce,
				wantBody:            body,
			})
		}
	}
}

func TestFileServerNegotiateEncoding(t *testing.T) {
	for _, tt := range getTests([]string{"/", "/foo.html"}) {
		path := tt.dir + tt.suffix
		wantEncoding, ext := expectedEncoding(tt.dir, tt.ae)
		var filepath string
		if tt.suffix == "/" {
			filepath = path[1:] + "index.html" + ext
		} else {
			filepath = path[1:] + ext
		}
		doTest(t, testData{
			path:                path,
			acceptEncoding:      tt.ae.ae,
			wantCode:            http.StatusOK,
			wantContentType:     "text/html; charset=utf-8",
			wantContentEncoding: wantEncoding,
			wantBody:            fsmap[filepath],
			wantVary:            "Accept-Encoding",
		})
	}
}

func expectedEncoding(dir string, ae ae) (string, string) {
	// Prioritize Brotli over Gzip
	if strings.Contains(dir, ".br") && ae.expectsBrotli {
		return "br", ".br"
	} else if strings.Contains(dir, ".gz") && ae.expectsGzip {
		return "gzip", ".gz"
	}
	return "", ""
}

type testData struct {
	path           string
	acceptEncoding string

	wantCode            int
	wantContentType     string
	wantContentEncoding string
	wantBody            string
	wantVary            string
}

func (t *testData) String() string {
	return t.path + "[" + t.acceptEncoding + "]"
}

func doTest(t *testing.T, tt testData) {
	req := httptest.NewRequest("GET", tt.path, nil)
	if tt.acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", tt.acceptEncoding)
	}
	rec := httptest.NewRecorder()

	fs.ServeHTTP(rec, req)

	if g, e := rec.Code, tt.wantCode; g != e {
		t.Errorf("test %s: status = %d, want %d", tt.String(), g, e)
	}
	if g, e := rec.Header().Get("Content-Type"), tt.wantContentType; g != e {
		t.Errorf("test %s: content-type = %q, want %q", tt.String(), g, e)
	}
	if g, e := rec.Header().Get("Content-Encoding"), tt.wantContentEncoding; g != e {
		t.Errorf("test %s: content-encoding = %q, want %q", tt.String(), g, e)
	}
	if g, e := rec.Body.String(), tt.wantBody; g != e {
		t.Errorf("test %s: body = %q, want %q", tt.String(), g, e)
	}
	if g, e := rec.Header().Get("Vary"), tt.wantVary; g != e {
		t.Errorf("test %s: vary = %q, want %q", tt.String(), g, e)
	}
}

func TestGetWriter(t *testing.T) {
	for _, ae := range aes {
		req := httptest.NewRequest("", "/", nil)
		if ae.ae != "" {
			req.Header.Set("Accept-Encoding", ae.ae)
		}
		rec := httptest.NewRecorder()
		w := GetWriter(rec, req)

		rec.WriteHeader(http.StatusNotFound)
		if rec.Code != http.StatusNotFound {
			t.Errorf("test %s: GetWriter triggered a write; cannot use WriteHeader afterwards", ae.ae)
		}

		io.WriteString(w, "Hello World!")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if c, ok := w.(io.Closer); ok {
			c.Close()
		}
		if ae.expectsGzip {
			if _, ok := w.(io.Closer); !ok {
				t.Errorf("test %s: GetWriter didn't return an io.Closer; got %s, wanted gzip.Writer", ae.ae, reflect.TypeOf(w).Name())
			}
			if r, err := gzip.NewReader(rec.Body); err != nil {
				t.Errorf("test %s: %v", ae.ae, err)
			} else if buf, err := ioutil.ReadAll(r); err != nil {
				t.Errorf("test %s: %v", ae.ae, err)
			} else if g, e := string(buf), "Hello World!"; g != e {
				t.Errorf("test %s: body = %q, want %q", ae.ae, g, e)
			}
		} else {
			if w != rec {
				t.Errorf("GetWriter didn't return the http.ResponseWriter directly; got %s, wanted httptest.ResponseRecorder", reflect.TypeOf(w).Name())
			}
			if g, e := rec.Body.String(), "Hello World!"; g != e {
				t.Errorf("test %s: body = %q, want %q", ae.ae, g, e)
			}
		}
	}
}

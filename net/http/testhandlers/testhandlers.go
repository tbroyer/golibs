// Copyright (c) 2016 Thomas Broyer. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package testhandlers provides http handlers that can be useful for setting
// up test servers (most likely with a net/http/httptest.Server).
//
// They're not meant to be used in production.
package testhandlers // import "go.ltgt.net/net/http/testhandlers"

import (
	"net/http"
	"strings"
	"time"
)

var sleep = time.Sleep

// Delay wraps an http.Handler to delay it based on the request's query-string,
// expecting a query parameter named 'delay' whose value is a duration (parsed
// using time.ParseDuration).
//
// The wrapped handler will be called immediately in case the 'delay' query
// parameter is absent or its value is malformed or negative.
func Delay(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if d := q.Get("delay"); d != "" {
			if dd, err := time.ParseDuration(d); err == nil {
				sleep(dd)
			}
		}
		h.ServeHTTP(w, r)
	})
}

// AddHeaders wraps an http.Handler to add response headers based on the
// request's query-string, expecting query parameters named 'header' whose
// value is an HTTP header line (header name and value separated by a colon.)
//
// The headers are added to the http.ResponseWriter before the wrapped handler
// is called (so the handler can alter those headers.)
func AddHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		for _, hhh := range q["header"] {
			hhhh := strings.SplitN(hhh, ":", 2)
			v := ""
			if len(hhhh) > 1 {
				v = hhhh[1]
			}
			w.Header().Add(hhhh[0], v)
		}
		h.ServeHTTP(w, r)
	})
}

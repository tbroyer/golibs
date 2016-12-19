// Copyright (c) 2016 Thomas Broyer. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testhandlers // import "go.ltgt.net/net/http/testhandlers"

import (
	"net/http"
	"strings"
	"time"
)

var sleep = time.Sleep

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

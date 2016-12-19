// Copyright (c) 2016 Thomas Broyer. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testhandlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDelay(t *testing.T) {
	var delay time.Duration
	var sleepCalled int
	sleep = func(d time.Duration) {
		sleepCalled++
		delay = d
	}
	var handlerCalled int
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled++
	})

	testData := []struct {
		target           string
		wantSleepSkipped bool
		wantDelay        time.Duration
	}{
		{
			target:           "/",
			wantSleepSkipped: true,
		},
		{
			target:           "/?delay=",
			wantSleepSkipped: true,
		},
		{
			target:           "/?delay=invalid",
			wantSleepSkipped: true,
		},
		{
			target:           "/?delay=10",
			wantSleepSkipped: true,
		},
		{
			target:           "/?delay=10msinvalid",
			wantSleepSkipped: true,
		},
		{
			target:    "/?delay=10ms",
			wantDelay: 10 * time.Millisecond,
		},
		{
			target:    "/?delay=3s30ms",
			wantDelay: 3*time.Second + 30*time.Millisecond,
		},
	}

	for _, tt := range testData {
		delay, sleepCalled, handlerCalled = 0, 0, 0

		req := httptest.NewRequest("", tt.target, nil)
		rec := httptest.NewRecorder()
		Delay(handler).ServeHTTP(rec, req)

		wantSleepCalled := 1
		if tt.wantSleepSkipped {
			wantSleepCalled = 0
		}
		if g, e := sleepCalled, wantSleepCalled; g != e {
			t.Errorf("test %q: sleep function called %d times, want %d", tt.target, g, e)
		} else if g, e := delay, tt.wantDelay; g != e {
			t.Errorf("test %q: delay = %s, want %s", tt.target, g, e)
		}

		if g, e := handlerCalled, 1; g != e {
			t.Errorf("test %q: wrapped handler called %d times, want %d", tt.target, g, e)
		}
	}
}

func TestAddHeaders(t *testing.T) {
	var handlerCalled int
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled++
	})

	testData := []struct {
		target       string
		dontWantFoo  bool
		wantFooValue []string
		dontWantBar  bool
		wantBarValue []string
	}{
		{
			target:      "/",
			dontWantFoo: true,
			dontWantBar: true,
		},
		{
			target:       "/?header=x-foo",
			wantFooValue: []string{""},
			dontWantBar:  true,
		},
		{
			target:       "/?header=x-foo:",
			wantFooValue: []string{""},
			dontWantBar:  true,
		},
		{
			target:       "/?header=x-foo:bar",
			wantFooValue: []string{"bar"},
			dontWantBar:  true,
		},
		{
			target:       "/?header=x-foo:bar&header=x-foo:baz",
			wantFooValue: []string{"bar", "baz"},
			dontWantBar:  true,
		},
		{
			target:       "/?header=x-foo:baz&header=x-bar:qux",
			wantFooValue: []string{"baz"},
			wantBarValue: []string{"qux"},
		},
	}
	for _, tt := range testData {
		handlerCalled = 0
		req := httptest.NewRequest("", tt.target, nil)
		rec := httptest.NewRecorder()
		AddHeaders(handler).ServeHTTP(rec, req)

		if h, ok := rec.HeaderMap["X-Foo"]; ok == tt.dontWantFoo || !areEqual(h, tt.wantFooValue) {
			t.Errorf("test %q: x-foo = %v, want %v", tt.target, h, tt.wantFooValue)
		}
		if h, ok := rec.HeaderMap["X-Bar"]; ok == tt.dontWantBar || !areEqual(h, tt.wantBarValue) {
			t.Errorf("test %q: x-bar = %v, want %v", tt.target, h, tt.wantBarValue)
		}

		if g, e := handlerCalled, 1; g != e {
			t.Errorf("test %q: wrapped handler called %d times, want %d", tt.target, g, e)
		}
	}
}

func areEqual(g, e []string) bool {
	if len(g) != len(e) {
		return false
	}
	for i, v := range g {
		if v != e[i] {
			return false
		}
	}
	return true
}

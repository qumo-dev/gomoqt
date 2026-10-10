package qmux

import (
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelectProtocol(t *testing.T) {
	tests := map[string]struct {
		offered   []string
		supported []string
		expected  string
		ok        bool
	}{
		"offered": {
			offered: []string{"qmux-02.moq-lite-05"}, supported: []string{"moq-lite-05"},
			expected: "moq-lite-05", ok: true,
		},
		"several in one header value, as browsers send them": {
			offered: []string{"qmux-02.moq-lite-05, qmux-01.moq-lite-05, webtransport"}, supported: []string{"moq-lite-05"},
			expected: "moq-lite-05", ok: true,
		},
		"the server's preference decides": {
			offered: []string{"qmux-02.a", "qmux-02.b"}, supported: []string{"b", "a"},
			expected: "b", ok: true,
		},
		"another QMux draft":               {offered: []string{"qmux-01.moq-lite-05"}, supported: []string{"moq-lite-05"}},
		"the QMux draft alone":             {offered: []string{"qmux-02"}, supported: []string{"moq-lite-05"}},
		"the application protocol alone":   {offered: []string{"moq-lite-05"}, supported: []string{"moq-lite-05"}},
		"another application protocol":     {offered: []string{"qmux-02.moq-lite-04"}, supported: []string{"moq-lite-05"}},
		"nothing offered":                  {offered: nil, supported: []string{"moq-lite-05"}},
		"nothing supported":                {offered: []string{"qmux-02.moq-lite-05"}, supported: nil},
		"an empty header value":            {offered: []string{""}, supported: []string{"moq-lite-05"}},
		"a prefix of a supported protocol": {offered: []string{"qmux-02.moq"}, supported: []string{"moq-lite-05"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := selectProtocol(tt.offered, tt.supported)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestIsUpgrade(t *testing.T) {
	tests := map[string]struct {
		method   string
		header   http.Header
		expected bool
	}{
		"upgrade": {
			method:   http.MethodGet,
			header:   http.Header{"Connection": {"Upgrade"}, "Upgrade": {"websocket"}},
			expected: true,
		},
		"tokens among others, in any case": {
			method:   http.MethodGet,
			header:   http.Header{"Connection": {"keep-alive, UPGRADE"}, "Upgrade": {"WebSocket"}},
			expected: true,
		},
		"plain request": {
			method: http.MethodGet,
			header: http.Header{},
		},
		"upgrade to something else": {
			method: http.MethodGet,
			header: http.Header{"Connection": {"Upgrade"}, "Upgrade": {"h2c"}},
		},
		"without the Connection token": {
			method: http.MethodGet,
			header: http.Header{"Upgrade": {"websocket"}},
		},
		"not a GET": {
			method: http.MethodPost,
			header: http.Header{"Connection": {"Upgrade"}, "Upgrade": {"websocket"}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := &http.Request{Method: tt.method, Header: tt.header}
			assert.Equal(t, tt.expected, IsUpgrade(r))
		})
	}
}

func TestParseAddr(t *testing.T) {
	tests := map[string]struct {
		input    string
		expected net.Addr
	}{
		"IPv4":              {input: "192.0.2.1:443", expected: &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1).To4(), Port: 443}},
		"IPv6":              {input: "[2001:db8::1]:443", expected: &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 443}},
		"not an IP":         {input: "example.com:443", expected: addr("example.com:443")},
		"empty":             {input: "", expected: addr("")},
		"pipe, as in tests": {input: "pipe", expected: addr("pipe")},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := parseAddr(tt.input)
			assert.Equal(t, tt.expected.String(), got.String())
			assert.IsType(t, tt.expected, got)
		})
	}
}

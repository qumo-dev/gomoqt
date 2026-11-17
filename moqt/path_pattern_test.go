package moqt

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePathPattern(t *testing.T) {
	tests := map[string]struct {
		input   string
		wantErr bool
	}{
		"root":                         {input: "/"},
		"path":                         {input: "/room/1/alice"},
		"prefix":                       {input: "/room/1/"},
		"empty":                        {input: "", wantErr: true},
		"no leading /":                 {input: "room/1", wantErr: true},
		"empty segment":                {input: "/room//1"},
		"empty root segment":           {input: "//"},
		"wildcard by an empty segment": {input: "/{id}//x"},
		"wildcard":                     {input: "/room/{id}/host"},
		"wildcard prefix":              {input: "/room/{id}/"},
		"rest wildcard":                {input: "/room/{rest...}"},
		"keyword name":                 {input: "/room/{type}"},
		"two wildcards":                {input: "/room/{id}/{name}"},
		"wildcard inside segment":      {input: "/room/a{id}", wantErr: true},
		"unclosed wildcard":            {input: "/room/{id", wantErr: true},
		"stray closing brace":          {input: "/room/id}", wantErr: true},
		"empty wildcard name":          {input: "/room/{}", wantErr: true},
		"non-identifier name":          {input: "/room/{1d}", wantErr: true},
		"duplicate name":               {input: "/{id}/{id}", wantErr: true},
		"rest wildcard not last":       {input: "/{rest...}/host", wantErr: true},
		"rest wildcard then /":         {input: "/{rest...}/", wantErr: true},
		"anchor":                       {input: "/room/{$}"},
		"anchor not last":              {input: "/{$}/room", wantErr: true},
		"anchor then /":                {input: "/room/{$}/", wantErr: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			p, err := ParsePathPattern(tt.input)
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrInvalidPathPattern)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.input, p.raw)
		})
	}
}

func TestPathPattern_match(t *testing.T) {
	tests := map[string]struct {
		pattern string
		path    BroadcastPath
		want    bool
	}{
		"path matches itself":                       {pattern: "/room/1/alice", path: "/room/1/alice", want: true},
		"path matches no other":                     {pattern: "/room/1/alice", path: "/room/1/bob", want: false},
		"path matches nothing beneath":              {pattern: "/room/1", path: "/room/1/alice", want: false},
		"prefix matches beneath":                    {pattern: "/room/1/", path: "/room/1/alice", want: true},
		"prefix matches deeper":                     {pattern: "/room/1/", path: "/room/1/a/b", want: true},
		"prefix stops at a / boundary":              {pattern: "/room/1/", path: "/room/12/alice", want: false},
		"prefix does not match its stem":            {pattern: "/room/1/", path: "/room/1", want: false},
		"root matches every path":                   {pattern: "/", path: "/anything/at/all", want: true},
		"wildcard matches one segment":              {pattern: "/room/{id}/host", path: "/room/1/host", want: true},
		"wildcard needs the literal after it":       {pattern: "/room/{id}/host", path: "/room/1/guest", want: false},
		"wildcard matches no empty segment":         {pattern: "/room/{id}", path: "/room/", want: false},
		"wildcard matches no two segments":          {pattern: "/room/{id}", path: "/room/1/host", want: false},
		"wildcard path matches nothing deeper":      {pattern: "/room/{id}/host", path: "/room/1/host/a", want: false},
		"wildcard prefix matches beneath":           {pattern: "/room/{id}/", path: "/room/1/alice", want: true},
		"wildcard prefix matches deeper":            {pattern: "/room/{id}/", path: "/room/1/a/b", want: true},
		"wildcard prefix needs a segment":           {pattern: "/room/{id}/", path: "/room/1", want: false},
		"wildcard needs its literal before":         {pattern: "/room/{id}/", path: "/lobby/1/alice", want: false},
		"rest wildcard matches one segment":         {pattern: "/room/{rest...}", path: "/room/1", want: true},
		"rest wildcard matches deeper":              {pattern: "/room/{rest...}", path: "/room/1/a/b", want: true},
		"rest wildcard matches an empty rest":       {pattern: "/room/{rest...}", path: "/room/", want: true},
		"rest wildcard needs its stem":              {pattern: "/room/{rest...}", path: "/room", want: false},
		"anchor matches its trailing /":             {pattern: "/room/{$}", path: "/room/", want: true},
		"anchor matches nothing beneath":            {pattern: "/room/{$}", path: "/room/1", want: false},
		"anchor needs its trailing /":               {pattern: "/room/{$}", path: "/room", want: false},
		"anchor at the root matches only /":         {pattern: "/{$}", path: "/room", want: false},
		"anchor after a wildcard":                   {pattern: "/room/{id}/{$}", path: "/room/1/", want: true},
		"empty segment matches itself":              {pattern: "/room//1", path: "/room//1", want: true},
		"empty segment matches no other":            {pattern: "/room//1", path: "/room/1", want: false},
		"empty segment prefix":                      {pattern: "//", path: "//x", want: true},
		"empty segment beside a wildcard":           {pattern: "/{id}//x", path: "/a//x", want: true},
		"wildcard matches no empty segment between": {pattern: "/room/{id}/x", path: "/room//x", want: false},
		"wildcard matches no invalid path":          {pattern: "/{id}", path: "room", want: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			p, err := ParsePathPattern(tt.pattern)
			require.NoError(t, err)

			assert.Equal(t, tt.want, p.Match(tt.path))
		})
	}
}

func TestPathPattern_Match_ZeroValue(t *testing.T) {
	var p PathPattern

	assert.False(t, p.Match("/"))
	assert.False(t, p.Match("/room"))
}

func TestParseGrantPattern(t *testing.T) {
	tests := map[string]struct {
		root, pattern string
		want          string
		wantErr       bool
	}{
		"a path beneath the root":  {root: "/room/1/", pattern: "/alice", want: "/room/1/alice"},
		"everything beneath":       {root: "/room/1/", pattern: "/", want: "/room/1/"},
		"a wildcard":               {root: "/room/", pattern: "/{id}/audio", want: "/room/{id}/audio"},
		"the root /":               {root: "/", pattern: "/x", want: "/x"},
		"a root not ending in /":   {root: "/room/1", pattern: "/alice", wantErr: true},
		"a root with a wildcard":   {root: "/{room}/", pattern: "/alice", wantErr: true},
		"a pattern not starting /": {root: "/room/1/", pattern: "alice", wantErr: true},
		"an empty root":            {root: "", pattern: "/alice", wantErr: true},
		"a malformed wildcard":     {root: "/room/", pattern: "/{id", wantErr: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			p, err := ParseGrantPattern(tt.root, tt.pattern)

			if tt.wantErr {
				assert.ErrorIs(t, err, ErrInvalidPathPattern)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, p.String())
		})
	}
}

func TestGrant_Validate(t *testing.T) {
	tests := map[string]struct {
		grant   Grant
		wantErr bool
	}{
		"a root with patterns":     {grant: Grant{Root: "/room/1/", Publish: []string{"/7"}, Subscribe: []string{"/"}}},
		"nothing":                  {grant: Grant{}},
		"patterns without root":    {grant: Grant{Subscribe: []string{"/"}}, wantErr: true},
		"a root that is no prefix": {grant: Grant{Root: "/room"}, wantErr: true},
		"a malformed pattern":      {grant: Grant{Root: "/", Publish: []string{"x"}}, wantErr: true},
		"a negative max age":       {grant: Grant{MaxAge: -1}, wantErr: true},
		"a max age":                {grant: Grant{Root: "/", Subscribe: []string{"/"}, MaxAge: time.Minute}},
		"too large for AUTH_OK":    {grant: Grant{Root: "/", Subscribe: manyPatterns(20000)}, wantErr: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.grant.Validate()

			assert.Equal(t, tt.wantErr, err != nil, "%v", err)
		})
	}
}

func TestGrant_Validate_Errors(t *testing.T) {
	assert.ErrorIs(t, Grant{Root: "/room"}.Validate(), ErrInvalidPathPattern, "a root that is no prefix")
	assert.ErrorIs(t, Grant{MaxAge: -1}.Validate(), ErrInvalidGrant)
	assert.ErrorIs(t, Grant{Root: "/", Subscribe: manyPatterns(20000)}.Validate(), ErrInvalidGrant)
}

// manyPatterns returns n distinct patterns.
func manyPatterns(n int) []string {
	patterns := make([]string, n)
	for i := range patterns {
		patterns[i] = fmt.Sprintf("/broadcast-%d", i)
	}
	return patterns
}

func TestPathPattern_CoversBeneath(t *testing.T) {
	tests := map[string]struct {
		pattern string
		route   BroadcastPath
		want    bool
	}{
		"prefix leading the route":            {pattern: "/live/", route: "/live", want: true},
		"prefix leading a deeper route":       {pattern: "/live/", route: "/live/alice", want: true},
		"prefix below the route":              {pattern: "/live/alice/", route: "/live", want: false},
		"exact path":                          {pattern: "/live", route: "/live", want: false},
		"prefix of another segment":           {pattern: "/live/", route: "/livex", want: false},
		"root prefix":                         {pattern: "/", route: "/live", want: true},
		"wildcard prefix":                     {pattern: "/room/{id}/", route: "/room/1", want: true},
		"wildcard prefix, route too short":    {pattern: "/room/{id}/", route: "/room", want: false},
		"rest wildcard":                       {pattern: "/room/{rest...}", route: "/room", want: true},
		"rest wildcard after a wildcard":      {pattern: "/room/{id}/{rest...}", route: "/room/1", want: true},
		"wildcard exact path":                 {pattern: "/room/{id}", route: "/room/1", want: false},
		"anchor":                              {pattern: "/room/{$}", route: "/room", want: false},
		"wildcard does not match empty field": {pattern: "/room/{id}/", route: "/room/", want: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			p, err := ParsePathPattern(tt.pattern)
			require.NoError(t, err)

			assert.Equal(t, tt.want, p.coversBeneath(tt.route))
		})
	}
}

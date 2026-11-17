package msf

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docsDir holds the msf pages of the documentation site.
const docsDir = "../docs/msf"

// docBlockPattern matches a fenced code block and captures its language and
// body.
var docBlockPattern = regexp.MustCompile("(?s)```(go|json)\n(.*?)```")

// exampleMarkerPattern matches the first line of a documented Go block that
// mirrors an Example function in example_test.go.
var exampleMarkerPattern = regexp.MustCompile(`^// Example: (\w+)\n`)

// docBlock is one fenced code block of a docs page.
type docBlock struct {
	page string
	lang string
	body string
}

// readDocBlocks returns the go and json code blocks of every docs page.
func readDocBlocks(tb testing.TB) []docBlock {
	tb.Helper()
	pages, err := filepath.Glob(filepath.Join(docsDir, "*.mdx"))
	require.NoError(tb, err)
	require.NotEmpty(tb, pages)
	var blocks []docBlock
	for _, page := range pages {
		data, err := os.ReadFile(page)
		require.NoError(tb, err)
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		for _, m := range docBlockPattern.FindAllStringSubmatch(text, -1) {
			blocks = append(blocks, docBlock{page: filepath.Base(page), lang: m[1], body: m[2]})
		}
	}
	return blocks
}

// exampleBodies returns the body of every Example function in
// example_test.go, up to its "// Output:" comment.
func exampleBodies(tb testing.TB) map[string]string {
	tb.Helper()
	src, err := os.ReadFile("example_test.go")
	require.NoError(tb, err)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "example_test.go", src, parser.ParseComments)
	require.NoError(tb, err)

	bodies := make(map[string]string)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(fn.Name.Name, "Example") {
			continue
		}
		body := string(src[fset.Position(fn.Body.Lbrace).Offset+1 : fset.Position(fn.Body.Rbrace).Offset])
		if i := strings.Index(body, "// Output:"); i >= 0 {
			body = body[:i]
		}
		bodies[fn.Name.Name] = body
	}
	return bodies
}

// withoutSpace drops every whitespace rune, so indentation and line breaks do
// not count when comparing code.
func withoutSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// TestDocs_GoBlocksMatchExamples checks that every documented Go block that
// names an Example function is that function's body, so the documented code
// compiles and its output is checked by go test.
func TestDocs_GoBlocksMatchExamples(t *testing.T) {
	bodies := exampleBodies(t)
	mirrored := 0
	for _, block := range readDocBlocks(t) {
		if block.lang != "go" {
			continue
		}
		m := exampleMarkerPattern.FindStringSubmatch(block.body)
		if m == nil {
			continue
		}
		mirrored++
		t.Run(block.page+"/"+m[1], func(t *testing.T) {
			body, ok := bodies[m[1]]
			require.True(t, ok, "example_test.go has no %s", m[1])
			assert.Equal(t, withoutSpace(body), withoutSpace(block.body[len(m[0]):]),
				"the block in %s differs from %s in example_test.go", block.page, m[1])
		})
	}
	assert.GreaterOrEqual(t, mirrored, 8, "the docs should mirror the examples")
}

// TestDocs_JSONBlocksValidate checks that every JSON block in the docs is a
// catalog or delta that parses and validates.
func TestDocs_JSONBlocksValidate(t *testing.T) {
	for i, block := range readDocBlocks(t) {
		if block.lang != "json" {
			continue
		}
		t.Run(block.page+"/"+itoa(i), func(t *testing.T) {
			data := []byte(block.body)
			if isDeltaJSON(data) {
				delta, err := ParseCatalogDelta(data)
				require.NoError(t, err)
				assert.NoError(t, delta.Validate())
				return
			}
			catalog, err := ParseCatalog(data)
			require.NoError(t, err)
			assert.NoError(t, catalog.Validate())
		})
	}
}

func TestDocs_NameTheCurrentDraft(t *testing.T) {
	pages, err := filepath.Glob(filepath.Join(docsDir, "*.mdx"))
	require.NoError(t, err)
	for _, page := range pages {
		data, err := os.ReadFile(page)
		require.NoError(t, err)
		for _, stale := range []string{"draft-ietf-moq-msf-01", "draft-ietf-moq-msf-00", "draft-00", "draft-01", "Version: 1"} {
			assert.NotContains(t, string(data), stale, filepath.Base(page))
		}
	}
}

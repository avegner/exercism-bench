package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"strings"
	"unicode"
)

const (
	codeStartPattern = "<pre class='line-numbers solution-code'><code class='language-go'>"
	codeEndPattern   = "</code></pre>"
)

type codeRange struct {
	start int
	end   int
}

type codeRanges []*codeRange

func (ranges *codeRanges) include(offset int) bool {
	for _, r := range *ranges {
		if offset >= r.start && offset < r.end {
			return true
		}
	}
	return false
}

func (ranges *codeRanges) add(start, end int) {
	*ranges = append(*ranges, &codeRange{
		start: start,
		end:   end,
	})
}

// getCodeSize returns number of symbols in code w/o white spaces and comments.
func getCodeSize(sourceFilePath string) (size uint, err error) {
	bs, err := ioutil.ReadFile(sourceFilePath)
	if err != nil {
		return
	}
	// exclude comments and ignore white spaces in string and char literals
	var (
		exclude, ignore codeRanges
	)
	// parse source code
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, sourceFilePath, bs, parser.ParseComments)
	if err != nil {
		return
	}
	// find all comments, string and char literals
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Comment:
			exclude.add(fs.Position(v.Pos()).Offset, fs.Position(v.End()).Offset)
		case *ast.BasicLit:
			if v.Kind == token.STRING || v.Kind == token.CHAR {
				ignore.add(fs.Position(v.Pos()).Offset, fs.Position(v.End()).Offset)
			}
		}
		return true
	})
	// count only relevant code symbols
	for i, r := range string(bs) {
		if exclude.include(i) {
			continue
		}
		if ignore.include(i) || !unicode.IsSpace(r) {
			size++
		}
	}
	return size, nil
}

func normalizeCode(content string) string {
	rm := map[string]string{
		"&amp;":  "&",
		"&quot;": "\"",
		"&lt;":   "<",
		"&gt;":   ">",
		"&#39;":  "'",
	}
	for old, new := range rm {
		content = strings.ReplaceAll(content, old, new)
	}
	return content
}

// TODO: refactor code extraction, decode html
func extractSolutionCode(content string) string {
	const noCode = "NO CODE"

	ind := strings.Index(content, codeStartPattern)
	if ind == -1 {
		return noCode
	}
	content = content[ind+len(codeStartPattern):]
	if ind = strings.Index(content, codeEndPattern); ind == -1 {
		return noCode
	}
	content = content[:ind]

	return normalizeCode(content)
}

// TODO: refactor test suite extraction
func extractTestSuite(content string) (tsm map[string]string, err error) {
	sind := strings.Index(content, "<div class='pane pane-2 test-suite'>")
	if sind == -1 {
		return nil, errors.New("no suite start")
	}
	eind := strings.Index(content[sind:], "</div>")
	if eind == -1 {
		return nil, errors.New("no suite end")
	}
	content = content[sind : sind+eind]
	tsm = make(map[string]string)

	for {
		fns := strings.Index(content, "<h3>")
		if fns == -1 {
			break
		}

		fne := strings.Index(content, "</h3>")
		if fne == -1 {
			return nil, errors.New("no test file name end")
		}
		cs := strings.Index(content, "package")
		if cs == -1 {
			return nil, errors.New("no test file start")
		}
		ce := strings.Index(content, "</code></pre>")
		if ce == -1 {
			return nil, errors.New("no test file end")
		}

		fn := content[fns+4 : fne]
		code := content[cs:ce]
		tsm[fn] = normalizeCode(code)

		content = content[ce+12:]
	}

	return tsm, nil
}

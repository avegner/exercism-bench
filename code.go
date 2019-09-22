package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"html"
	"io/ioutil"
	"regexp"
	"strings"
	"unicode"
)

const (
	testSuiteStartPattern    = "<div class='pane pane-2 test-suite'>"
	testSuiteEndPattern      = "</div>"
	codeStartPattern         = "<code class='language-go'>"
	codeEndPattern           = "</code>"
	solutionCodeStartPattern = "<pre class='line-numbers solution-code'>" + codeStartPattern
	solutionCodeEndPattern   = codeEndPattern + "</pre>"
	testFileNameStartPattern = "<h3>"
	testFileNameEndPattern   = "</h3>"
)

var (
	authorRE = regexp.MustCompile("Avatar of (([[:word:]]|-)+)")
)

var (
	errNoSolutionCode = errors.New("no solution code")
	errNoAuthorName   = errors.New("no author name")
	errNoTestSuite    = errors.New("no test suite")
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

func extractSolutionCode(solutionPage string) (code, author string, err error) {
	// extract author name
	ms := authorRE.FindStringSubmatch(solutionPage)
	if ms == nil {
		return "", "", errNoAuthorName
	}
	author = html.UnescapeString(ms[1])

	// extract code
	m, _ := getFirstMatch(solutionPage, solutionCodeStartPattern, solutionCodeEndPattern)
	if m == "" {
		return "", "", errNoSolutionCode
	}
	code = html.UnescapeString(m)

	return code, author, nil
}

func extractTestSuite(solutionPage string) (suite map[string]string, err error) {
	// locate test suite
	ts, _ := getFirstMatch(solutionPage, testSuiteStartPattern, testSuiteEndPattern)
	if ts == "" {
		return nil, errNoTestSuite
	}

	// extract test files
	suite = make(map[string]string)
	for {
		// locate file name
		var m string
		m, ts = getFirstMatch(ts, testFileNameStartPattern, testFileNameEndPattern)
		if m == "" {
			if len(suite) == 0 {
				return nil, errNoTestSuite
			}
			break
		}
		name := html.UnescapeString(m)

		// locate code
		m, ts = getFirstMatch(ts, codeStartPattern, codeEndPattern)
		if m == "" {
			return nil, errNoTestSuite
		}
		code := html.UnescapeString(m)

		// fill in suite
		suite[name] = code
	}

	return suite, nil
}

// getFirstMatch looks for a substring with given start and end patterns.
// match contains the substring excluding patterns or empty string if nothing has been found.
// out gets the remaining input string after the chunk and the end pattern.
func getFirstMatch(in, sp, ep string) (match, out string) {
	sind := strings.Index(in, sp)
	if sind == -1 {
		return
	}
	sind += len(sp)

	eind := strings.Index(in[sind:], ep)
	if eind == -1 {
		return
	}

	return in[sind : sind+eind], in[sind+eind+len(ep):]
}

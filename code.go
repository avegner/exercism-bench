package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"unicode"
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

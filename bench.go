package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

var (
	benchNameRE  = regexp.MustCompile("Benchmark([[:alnum:]]|_)+")
	benchStatsRE = regexp.MustCompile(`[[:digit:]]+ ns/op\s+[[:digit:]]+ B/op\s+[[:digit:]]+ allocs/op`)
)

type benchStats struct {
	time   uint // ns
	mem    uint // B
	allocs uint
}

type solutionStats struct {
	name   string
	benchs map[string]*benchStats
	size   uint // symbols except comments and white spaces
}

// sort sorts by time (the most important), mem, allocs and size (the least).
func sortStatsByBench(sstats []*solutionStats, benchName string) {
	sort.SliceStable(sstats, func(i, j int) bool {
		lh, rh := sstats[i].benchs[benchName], sstats[j].benchs[benchName]
		return lh.time < rh.time ||
			(lh.time == rh.time &&
				lh.mem < rh.mem) ||
			(lh.time == rh.time &&
				lh.mem == rh.mem &&
				lh.allocs < rh.allocs) ||
			(lh.time == rh.time &&
				lh.mem == rh.mem &&
				lh.allocs == rh.allocs &&
				sstats[i].size < sstats[j].size)
	})
}

// getBenchNames looks for benchmark names in test suite files.
// All nested dirs in test suite dir are ignored.
func getBenchNames(testSuitePath string) (bnames []string, err error) {
	err = filepath.Walk(testSuitePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path == testSuitePath {
				return nil
			}
			return filepath.SkipDir
		}
		// read each test file
		bs, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}
		bnames = append(bnames, benchNameRE.FindAllString(string(bs), -1)...)
		return nil
	})
	return
}

// runBench runs benchmarks matching pattern in a given dir.
func runBench(dirPath, pattern string, arg ...string) (bstats map[string]*benchStats, err error) {
	// default pattern
	if pattern == "" {
		pattern = "."
	}
	// run benchmarks with tests
	out, err := runCmd("go", dirPath, append([]string{"test", "-bench", pattern, "-benchmem"}, arg...)...)
	if err != nil {
		return
	}
	// extract stats
	names := benchNameRE.FindAllString(out, -1)
	stats := benchStatsRE.FindAllString(out, -1)
	if len(names) != len(stats) {
		panic("len(names) != len(stats)")
	}
	bstats = make(map[string]*benchStats, len(names))
	for i, stat := range stats {
		st := &benchStats{}
		if _, err = fmt.Sscanf(stat, "%d ns/op %d B/op %d allocs/op", &st.time, &st.mem, &st.allocs); err != nil {
			return
		}
		bstats[names[i]] = st
	}
	return bstats, nil
}

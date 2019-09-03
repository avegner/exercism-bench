package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"sort"
)

var (
	benchNameRE       = regexp.MustCompile("Benchmark([[:alnum:]]|_)+")
	benchTimeRE       = regexp.MustCompile("[[:digit:]]+(.[[:digit:]]+)? ns/op")
	benchThroughputRE = regexp.MustCompile("[[:digit:]]+(.[[:digit:]]+)? MB/s")
	benchMemRE        = regexp.MustCompile(`[[:digit:]]+ B/op\s+[[:digit:]]+ allocs/op`)
	benchStatsRE      = regexp.MustCompile(
		fmt.Sprintf("%s(-[[:digit:]]+)?\\s+[[:digit:]]+\\s+%s\\s+(%s\\s+)?(%s)?",
			benchNameRE, benchTimeRE, benchThroughputRE, benchMemRE))
)

type benchStats struct {
	time       float64 // ns
	throughput float64 // MB
	mem        int     // B
	allocs     int
}

func (st *benchStats) String() string {
	s := fmt.Sprintf("%15.1f ns", st.time)
	if st.throughput != -1 {
		s += fmt.Sprintf(" %18.1f MB/s", st.throughput)
	}
	if st.mem != -1 && st.allocs != -1 {
		s += fmt.Sprintf(" %15d B mem %15d allocs", st.mem, st.allocs)
	}
	return s
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
				lh.throughput < rh.throughput) ||
			(lh.time == rh.time &&
				lh.throughput == rh.throughput &&
				lh.mem < rh.mem) ||
			(lh.time == rh.time &&
				lh.throughput == rh.throughput &&
				lh.mem == rh.mem &&
				lh.allocs < rh.allocs) ||
			(lh.time == rh.time &&
				lh.throughput == rh.throughput &&
				lh.mem == rh.mem &&
				lh.allocs == rh.allocs &&
				sstats[i].size < sstats[j].size)
	})
}

// getBenchNames looks for benchmark names in test suite files.
// All nested dirs in test suite dir are ignored.
func getBenchNames(testSuitePath string) (names []string, err error) {
	fis, err := ioutil.ReadDir(testSuitePath)
	if err != nil {
		return nil, err
	}
	for _, fi := range fis {
		if !regular(fi) {
			continue
		}
		// read each test file
		bs, err := ioutil.ReadFile(filepath.Join(testSuitePath, fi.Name()))
		if err != nil {
			return nil, err
		}
		names = append(names, benchNameRE.FindAllString(string(bs), -1)...)
	}
	return names, nil
}

// runBench runs benchmarks matching pattern in a given dir.
func runBench(dirPath, pattern string) (bstats map[string]*benchStats, err error) {
	// default pattern
	if pattern == "" {
		pattern = "."
	}
	// run benchmarks with tests
	out, err := runCmd("go", dirPath, "test", "-bench", pattern, "-benchmem")
	if err != nil {
		return
	}
	// extract stats
	lines := benchStatsRE.FindAllString(out, -1)
	if len(lines) == 0 {
		err = errors.New("no benchmarks")
		return
	}
	bstats = make(map[string]*benchStats, len(lines))
	for _, l := range lines {
		st := &benchStats{
			throughput: -1,
			mem:        -1,
			allocs:     -1,
		}
		name := benchNameRE.FindString(l)
		time := benchTimeRE.FindString(l)
		_, _ = fmt.Sscanf(time, "%f ns/op", &st.time)
		if throughput := benchThroughputRE.FindString(l); throughput != "" {
			_, _ = fmt.Sscanf(throughput, "%f MB/s", &st.throughput)
		}
		if mem := benchMemRE.FindString(l); mem != "" {
			_, _ = fmt.Sscanf(mem, "%d B/op %d allocs/op", &st.mem, &st.allocs)
		}
		bstats[name] = st
	}
	return bstats, nil
}

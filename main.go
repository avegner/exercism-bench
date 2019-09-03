package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"sync"
)

const (
	exercismAddr = "https://exercism.io"
	trackLang    = "go"
)

var commands = map[string]func(tq chan<- task, args []string) error{
	"total":    totalCmd,
	"download": downloadCmd,
	"bench":    benchCmd,
	"clean":    cleanCmd,
}

var (
	exerciseFlag    = ""
	downloadDirFlag = "./solutions"
	concurrencyFlag = true
	threadsFlag     = runtime.GOMAXPROCS(0)
)

var (
	uuidPattern            = "([[:xdigit:]][[:xdigit:]]){16}"
	solutionPathRE         = regexp.MustCompile("solutions/" + uuidPattern)
	uuidRE                 = regexp.MustCompile(uuidPattern)
	decimalNumberPattern   = "[[:digit:]]+"
	solutionGroupsNumberRE = regexp.MustCompile("solutions\\?page=" + decimalNumberPattern + "\">Last")
	decimalNumberRE        = regexp.MustCompile(decimalNumberPattern)
)

var errInvalidUsage = errors.New("invalid usage")

var mlog = log.New(os.Stderr, "", 0)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s -exercise=<name> [opt-flag...] COMMAND

Commands:
  total
  	calculate total number of published solutions
  download
  	download published solutions
  bench
  	bench downloaded solutions
  clean
  	remove downloaded solutions

Flags:
`, filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}
	flag.StringVar(&exerciseFlag, "exercise", exerciseFlag, "exercise name")
	flag.StringVar(&downloadDirFlag, "download-dir", downloadDirFlag, "directory for downloaded solutions")
	flag.BoolVar(&concurrencyFlag, "concurrency", concurrencyFlag, "enable concurrency")
	flag.IntVar(&threadsFlag, "threads", threadsFlag, "number of threads")
	flag.Parse()

	if err := run(flag.Args()); err != nil {
		if err == errInvalidUsage {
			flag.Usage()
			os.Exit(2)
		}
		mlog.Printf("run error: %v", err)
		os.Exit(1)
	}
}

func run(args []string) (err error) {
	if len(args) < 1 {
		return errInvalidUsage
	}
	if exerciseFlag == "" {
		return errInvalidUsage
	}
	cmd, ok := commands[args[0]]
	if !ok {
		return errInvalidUsage
	}

	// create pool of general purpose workers
	procs := 1
	if concurrencyFlag {
		runtime.GOMAXPROCS(threadsFlag)
		procs = runtime.GOMAXPROCS(0)
	}
	tq := make(chan task, procs)
	defer close(tq)
	for i := 0; i < procs; i++ {
		go worker(tq)
	}

	return cmd(tq, args[1:])
}

type task func()

func worker(wq <-chan task) {
	for {
		t, ok := <-wq
		if !ok {
			return
		}
		t()
	}
}

func totalCmd(tq chan<- task, args []string) error {
	if len(args) != 0 {
		return errInvalidUsage
	}
	paths, err := getSolutionPaths(tq)
	if err != nil {
		return err
	}
	mlog.Printf("solutions total: %d", len(paths))
	return nil
}

func benchCmd(tq chan<- task, args []string) error {
	if len(args) != 0 {
		return errInvalidUsage
	}
	// get benchmark names
	benchs, err := getBenchNames(makePath("test-suite"))
	if err != nil {
		return err
	}
	if len(benchs) == 0 {
		return errors.New("found 0 benchmarks")
	}
	mlog.Printf("found %d benchmarks:", len(benchs))
	for _, n := range benchs {
		mlog.Printf("- %s", n)
	}
	mlog.Println()
	// run all benches in test suite for all solutions
	wg := sync.WaitGroup{}
	sstats := []*solutionStats{}
	mx := sync.Mutex{}
	fis, err := ioutil.ReadDir(makePath())
	if err != nil {
		return err
	}
	for _, fi := range fis {
		if !regular(fi) {
			continue
		}
		// enqueue benchmarking task
		wg.Add(1)
		sn := fi.Name()
		tq <- func() {
			defer wg.Done()
			// create temp dir
			tmp, err := ioutil.TempDir("", "")
			if err != nil {
				mlog.Printf("temp dir create error: %v", err)
				return
			}
			defer os.RemoveAll(tmp)
			// copy all required files to temp dir
			dpath := filepath.Join(tmp, sn)
			if err = copyFile(makePath(sn), dpath); err != nil {
				mlog.Printf("copy file error: %v", err)
				return
			}
			if err = copyFiles(makePath("test-suite"), tmp); err != nil {
				mlog.Printf("copy test suite files error: %v", err)
				return
			}
			// run bench
			bstats, err := runBench(tmp, ".")
			if err != nil {
				mlog.Printf("%s: %v", sn, err)
				return
			}
			// prepare stats
			sz, err := getCodeSize(dpath)
			if err != nil {
				mlog.Printf("%s: %v", sn, err)
				return
			}
			st := &solutionStats{
				name:   sn,
				benchs: bstats,
				size:   sz,
			}
			mx.Lock()
			sstats = append(sstats, st)
			mx.Unlock()
			// progress
			mlog.Printf("%s: ok", st.name)
		}
	}
	// wait all tasks
	wg.Wait()
	// print stats in sorted way
	mlog.Println()
	for _, bn := range benchs {
		mlog.Printf("------------------------------ %s ------------------------------", bn)
		mlog.Println()
		sortStatsByBench(sstats, bn)
		for i, st := range sstats {
			mlog.Printf("[%5d] %s: %s %15d symbols",
				i, st.name, st.benchs[bn], st.size)
		}
		mlog.Println()
	}
	return nil
}

func downloadCmd(tq chan<- task, args []string) error {
	if len(args) != 0 {
		return errInvalidUsage
	}
	paths, err := getSolutionPaths(tq)
	if err != nil {
		return err
	}
	mlog.Printf("solutions total: %d", len(paths))
	count := 0
	mx := sync.Mutex{}
	if err = getSolutionCodes(tq, paths, func(uuid string) {
		mx.Lock()
		count++
		c := count
		mx.Unlock()
		mlog.Printf("downloaded %s: %5d / %5d - %5.1f%%",
			uuid, c, len(paths), float32(c)/float32(len(paths))*100)
	}); err != nil {
		return err
	}
	return nil
}

func cleanCmd(_ chan<- task, args []string) error {
	if len(args) != 0 {
		return errInvalidUsage
	}
	cp := makePath()
	if err := os.RemoveAll(cp); err != nil {
		return err
	}
	mlog.Printf("%s removed", cp)
	return nil
}

func makePath(path ...string) string {
	return filepath.Join(append([]string{downloadDirFlag, trackLang, exerciseFlag}, path...)...)
}

type pathMap map[string]struct{}

func getSolutionPaths(tq chan<- task) (paths pathMap, err error) {
	paths = make(pathMap)
	// get first solutions group page
	firstGroupPage, solutionsURL, err := getContent("solutions")
	if err != nil {
		err = fmt.Errorf("download of %s failed: %v", solutionsURL, err)
		return
	}
	// get total of solutions pages
	total, err := strconv.ParseUint(
		decimalNumberRE.FindString(
			solutionGroupsNumberRE.FindString(firstGroupPage)), 10, 32)
	if err != nil {
		return
	}
	// schedule downloads
	wg := sync.WaitGroup{}
	mx := sync.Mutex{}
	for i := uint64(0); i < total; i++ {
		n := i
		wg.Add(1)
		tq <- func() {
			defer wg.Done()
			// get solution group page
			groupPage, groupURL, err := getContent(fmt.Sprintf("solutions?page=%d", n+1))
			if err != nil {
				mlog.Printf("download of %s failed: %v", groupURL, err)
				return
			}
			// get solution paths
			for _, p := range solutionPathRE.FindAllString(groupPage, -1) {
				// ignore duplicates if they appear
				mx.Lock()
				paths[p] = struct{}{}
				mx.Unlock()
			}
		}
	}
	// wait all tasks
	wg.Wait()
	return paths, nil
}

func getSolutionCodes(tq chan<- task, paths pathMap, got func(uuid string)) error {
	if err := os.MkdirAll(makePath(), 0700); err != nil {
		return err
	}
	// get test suite
	for path := range paths {
		solutionPage, solutionURL, err := getContent(path)
		if err != nil {
			mlog.Printf("download of test suite %s failed: %v", solutionURL, err)
			return err
		}
		// store test suite
		ts, _ := extractTestSuite(solutionPage)
		tsp := makePath("test-suite")
		_ = os.Mkdir(tsp, 0700)
		for fn, fc := range ts {
			fp := filepath.Join(tsp, fn)
			if err := ioutil.WriteFile(fp, []byte(fc), 0600); err != nil {
				mlog.Printf("write of test file %s failed: %v", fp, err)
			}
		}
		break
	}
	// schedule downloads and stores
	wg := sync.WaitGroup{}
	for p := range paths {
		path := p
		wg.Add(1)
		tq <- func() {
			defer wg.Done()
			// get solution page
			solutionPage, solutionURL, err := getContent(path)
			if err != nil {
				mlog.Printf("download of %s failed: %v", solutionURL, err)
				return
			}
			// store solution code
			uuid := uuidRE.FindString(path)
			fp := makePath(uuid + ".go")
			if err := ioutil.WriteFile(fp, []byte(extractSolutionCode(solutionPage)), 0600); err != nil {
				mlog.Printf("write of %s failed: %v", fp, err)
			}
			got(uuid)
		}
	}
	// wait all tasks
	wg.Wait()
	return nil
}

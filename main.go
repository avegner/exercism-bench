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
	exercise        = ""
	downloadDirFlag = "./solutions"
	concurrencyFlag = false
	maxProcsFlag    = runtime.GOMAXPROCS(0)
)

var (
	solutionPathRE         = regexp.MustCompile("solutions/(([[:xdigit:]][[:xdigit:]]){16})")
	solutionGroupsNumberRE = regexp.MustCompile(`solutions\?page=([[:digit:]]+)">Last`)
)

var errInvalidUsage = errors.New("invalid usage")

var mlog = log.New(os.Stderr, "", 0)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s [flag...] <exercise-name> <command>

Commands:
  total
  	calculate number of published solutions
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
	flag.StringVar(&downloadDirFlag, "d", downloadDirFlag, "directory to store solutions")
	flag.BoolVar(&concurrencyFlag, "c", concurrencyFlag, "enable concurrency")
	flag.IntVar(&maxProcsFlag, "mp", maxProcsFlag, "GOMAXPROCS value to set")
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
	// check args
	if len(args) < 2 {
		return errInvalidUsage
	}
	exercise = args[0]
	cmd, ok := commands[args[1]]
	if !ok {
		return errInvalidUsage
	}

	// determine task queue size
	tqSize := 1
	runtime.GOMAXPROCS(maxProcsFlag)
	if concurrencyFlag {
		tqSize = runtime.GOMAXPROCS(0)
	}

	// create task queue and pool of general purpose workers
	tq := make(chan task, tqSize)
	defer close(tq)
	for i := 0; i < tqSize; i++ {
		go worker(tq)
	}

	// run a given command
	return cmd(tq, args[2:])
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

	uuids, err := getSolutionUUIDs(tq)
	if err != nil {
		return err
	}
	mlog.Printf("solutions total: %d", len(uuids))

	return nil
}

func benchCmd(tq chan<- task, args []string) error {
	if len(args) != 0 {
		return errInvalidUsage
	}

	// get benchmark names
	bnames, err := getBenchNames(solutionsDir("test-suite"))
	if err != nil {
		return err
	}
	if len(bnames) == 0 {
		return errors.New("found 0 benchmarks")
	}
	mlog.Printf("found %d benchmarks:", len(bnames))
	for _, n := range bnames {
		mlog.Printf("- %s", n)
	}
	mlog.Println()

	// get solutions total
	fis, err := ioutil.ReadDir(solutionsDir())
	if err != nil {
		return err
	}
	// TODO: replace with a precise count of solutions?
	// by default all files except test suite dir are considered as a solution code
	total := len(fis) - 1
	if total <= 0 {
		return errors.New("found 0 solutions")
	}
	mlog.Printf("solutions total: %d", total)
	mlog.Println()

	wg := sync.WaitGroup{}
	sstats := []*solutionStats{}
	mx := sync.Mutex{}

	// run all benches in test suite for all solutions
	for _, fi := range fis {
		if !regular(fi) {
			continue
		}

		// enqueue bench task
		wg.Add(1)
		fname := fi.Name()

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
			dpath := filepath.Join(tmp, fname)
			if err = copyFile(solutionsDir(fname), dpath); err != nil {
				mlog.Printf("copy file error: %v", err)
				return
			}
			if err = copyFiles(solutionsDir("test-suite"), tmp); err != nil {
				mlog.Printf("copy test suite files error: %v", err)
				return
			}

			// run bench
			bstats, err := runBench(tmp, ".")
			if err != nil {
				mlog.Printf("bench of %s failed: %v", fname, err)
				return
			}

			// prepare stats
			size, err := getCodeSize(dpath)
			if err != nil {
				mlog.Printf("bench of %s failed: %v", fname, err)
				return
			}
			st := &solutionStats{
				name:   fname,
				bstats: bstats,
				size:   size,
			}

			mx.Lock()
			sstats = append(sstats, st)
			count := len(sstats)
			mx.Unlock()

			// report progress
			mlog.Printf("benched %-64s: %5d / %5d - %5.1f%%",
				st.name, count, total, float32(count)/float32(total)*100)
		}
	}

	// wait all tasks
	wg.Wait()

	// print stats in sorted way
	mlog.Println()
	for _, bn := range bnames {
		mlog.Printf("------------------------------ %s ------------------------------", bn)
		mlog.Println()
		sortSolutionStatsByBench(sstats, bn)
		for i, st := range sstats {
			mlog.Printf("[%5d] %-64s: %s %15d symbols",
				i+1, st.name, st.bstats[bn], st.size)
		}
		mlog.Println()
	}

	return nil
}

func downloadCmd(tq chan<- task, args []string) error {
	if len(args) != 0 {
		return errInvalidUsage
	}

	// get all paths
	uuids, err := getSolutionUUIDs(tq)
	if err != nil {
		return err
	}
	mlog.Printf("solutions total: %d", len(uuids))
	mlog.Println()

	// download each solution
	count := 0
	mx := sync.Mutex{}

	if err = getSolutionCodes(tq, uuids, func(uuid, author string) {
		mx.Lock()
		count++
		c := count
		mx.Unlock()
		mlog.Printf("downloaded %s of %-32s: %5d / %5d - %5.1f%%",
			uuid, author, c, len(uuids), float32(c)/float32(len(uuids))*100)
	}); err != nil {
		return err
	}

	return nil
}

func cleanCmd(_ chan<- task, args []string) error {
	if len(args) != 0 {
		return errInvalidUsage
	}

	cp := solutionsDir()
	if err := os.RemoveAll(cp); err != nil {
		return err
	}
	mlog.Printf("%s removed", cp)

	return nil
}

func solutionsDir(path ...string) string {
	return filepath.Join(append([]string{downloadDirFlag, trackLang, exercise}, path...)...)
}

type uuidMap map[string]struct{}

func getSolutionUUIDs(tq chan<- task) (uuids uuidMap, err error) {
	// get first solutions group page
	firstGroupPage, solutionsURL, err := getSolutionPage("", nil)
	if err != nil {
		err = fmt.Errorf("download of %s failed: %v", solutionsURL, err)
		return
	}

	// get total of solutions pages
	ms := solutionGroupsNumberRE.FindStringSubmatch(firstGroupPage)
	if ms == nil {
		err = errors.New("can't find solution groups number")
		return
	}
	total, err := strconv.ParseUint(ms[1], 10, 64)
	if err != nil {
		return
	}

	// schedule downloads
	wg := sync.WaitGroup{}
	mx := sync.Mutex{}
	uuids = make(uuidMap)

	for i := uint64(0); i < total; i++ {
		n := i
		wg.Add(1)

		tq <- func() {
			defer wg.Done()

			// get solution group page
			groupPage, groupURL, err := getSolutionPage("", map[string]string{
				"page": strconv.FormatUint(n+1, 10),
			})
			if err != nil {
				mlog.Printf("download of %s failed: %v", groupURL, err)
				return
			}

			// get solution UUIDs
			mss := solutionPathRE.FindAllStringSubmatch(groupPage, -1)
			if mss == nil {
				mlog.Printf("can't find solution UUIDs in %s", groupURL)
				return
			}
			for _, ms := range mss {
				// ignore duplicates if they appear
				mx.Lock()
				uuids[ms[1]] = struct{}{}
				mx.Unlock()
			}
		}
	}

	// wait all tasks
	wg.Wait()

	return uuids, nil
}

func getSolutionCodes(tq chan<- task, uuids uuidMap, got func(uuid, author string)) error {
	if err := os.MkdirAll(solutionsDir(), 0700); err != nil {
		return err
	}

	// get test suite
	for uuid := range uuids {
		solutionPage, solutionURL, err := getSolutionPage(uuid, nil)
		if err != nil {
			mlog.Printf("download of test suite %s failed: %v", solutionURL, err)
			return err
		}

		// store test suite
		ts, err := extractTestSuite(solutionPage)
		if err != nil {
			return err
		}
		tsp := solutionsDir("test-suite")
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

	for k := range uuids {
		uuid := k
		wg.Add(1)

		tq <- func() {
			defer wg.Done()

			// get solution page
			solutionPage, solutionURL, err := getSolutionPage(uuid, nil)
			if err != nil {
				mlog.Printf("download of %s failed: %v", solutionURL, err)
				return
			}

			// extract solution code
			code, author, err := extractSolutionCode(solutionPage)
			if err != nil {
				mlog.Printf("code extraction for %s failed: %v", solutionURL, err)
				return
			}

			// store solution code
			fp := solutionsDir(uuid + "-" + author + ".go")
			if err := ioutil.WriteFile(fp, []byte(code), 0600); err != nil {
				mlog.Printf("write of %s failed: %v", fp, err)
			}
			got(uuid, author)
		}
	}

	// wait all tasks
	wg.Wait()

	return nil
}

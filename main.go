package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

const (
	exercismAddr = "https://exercism.io"
	trackLang    = "go"
)

var commands = map[string]func(tq chan<- task) error{
	"total":    totalCmd,
	"download": downloadCmd,
	"bench":    benchCmd,
	"clean":    cleanCmd,
}

var (
	exerciseFlag    = ""
	downloadDirFlag = "./solutions"
	concurrencyFlag = true
)

var (
	uuidPattern            = "([[:xdigit:]][[:xdigit:]]){16}"
	solutionPathRE         = regexp.MustCompile("solutions/" + uuidPattern)
	uuidRE                 = regexp.MustCompile(uuidPattern)
	decimalNumberPattern   = "[[:digit:]]+"
	solutionGroupsNumberRE = regexp.MustCompile("solutions\\?page=" + decimalNumberPattern + "\">Last")
	decimalNumberRE        = regexp.MustCompile(decimalNumberPattern)
	codeStartPattern       = "<pre class='line-numbers solution-code'><code class='language-go'>"
	codeEndPattern         = "</code></pre>"
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
	if len(args) != 1 {
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
		procs = runtime.GOMAXPROCS(0)
	}
	tq := make(chan task, procs)
	defer close(tq)
	for i := 0; i < procs; i++ {
		go worker(tq)
	}

	return cmd(tq)
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

//nolint:gosec
func getContent(path string) (content string, url string, err error) {
	url = strings.Join([]string{exercismAddr, "tracks", trackLang, "exercises", exerciseFlag, path}, "/")

	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("status code %q", resp.Status)
		return
	}

	bs, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}
	return string(bs), url, nil
}

func makePath(path ...string) string {
	return filepath.Join(append([]string{downloadDirFlag, trackLang, exerciseFlag}, path...)...)
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

func getSolutionCodes(tq chan<- task, paths pathMap) error {
	storePath := makePath()
	if err := os.MkdirAll(storePath, 0700); err != nil {
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
		tsp := filepath.Join(storePath, "test-suite")
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
			fp := makePath(uuidRE.FindString(path) + ".go")
			if err := ioutil.WriteFile(fp, []byte(extractSolutionCode(solutionPage)), 0600); err != nil {
				mlog.Printf("write of %s failed: %v", fp, err)
			}
		}
	}

	// wait all tasks
	wg.Wait()
	return nil
}

func totalCmd(tq chan<- task) error {
	paths, err := getSolutionPaths(tq)
	if err != nil {
		return err
	}
	mlog.Printf("solutions total: %d", len(paths))
	return nil
}

func benchCmd(tq chan<- task) error {
	// walk through all solutions
	solutionsPath := makePath()
	// get benchmark names
	benchs, err := getBenchNames(filepath.Join(solutionsPath, "test-suite"))
	if err != nil {
		return err
	}
	if len(benchs) == 0 {
		return errors.New("found 0 benches")
	}
	for _, bn := range benchs {
		mlog.Printf("found %s benchmark", bn)
	}
	// run all benches in test suite for all solutions
	// run each bench separately for all solutions
	wg := sync.WaitGroup{}
	sstats := []*solutionStats{}
	mx := sync.Mutex{}
	if err := filepath.Walk(solutionsPath, func(spath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if solutionsPath == spath {
				return nil
			}
			return filepath.SkipDir
		}
		// enqueue benchmarking task
		wg.Add(1)
		tq <- func() {
			defer wg.Done()
			// create temp dir
			tmp, err := ioutil.TempDir("", "")
			if err != nil {
				return
			}
			defer os.RemoveAll(tmp)
			// copy all required files to temp dir
			fn := filepath.Base(spath)
			dpath := filepath.Join(tmp, filepath.Base(spath))
			if err = copyFile(spath, dpath); err != nil {
				return
			}
			if err = copyFiles(makePath("test-suite"), tmp); err != nil {
				return
			}
			// run bench
			bstats, err := runBench(tmp, ".")
			if err != nil {
				mlog.Printf("%s: %v", fn, err)
				return
			}
			// prepare stats
			sz, err := getCodeSize(dpath)
			if err != nil {
				return
			}
			st := &solutionStats{
				name:   fn,
				benchs: bstats,
				size:   sz,
			}
			mx.Lock()
			sstats = append(sstats, st)
			mx.Unlock()
			// progress
			mlog.Printf("%s: ok", st.name)
		}

		return nil
	}); err != nil {
		return err
	}

	// wait all tasks
	wg.Wait()
	// print stats in sorted way
	mlog.Println()
	for _, bn := range benchs {
		sortStatsByBench(sstats, bn)
		mlog.Printf("------------------------------ %s ------------------------------", bn)
		mlog.Println()
		for i, st := range sstats {
			mlog.Printf("[%4d] %s: %9d ns, %9d B mem, %9d allocs, %9d symbols",
				i, st.name, st.benchs[bn].time, st.benchs[bn].mem, st.benchs[bn].allocs, st.size)
		}
		mlog.Println()
	}

	return nil
}

func downloadCmd(tq chan<- task) error {
	paths, err := getSolutionPaths(tq)
	if err != nil {
		return err
	}
	if err = getSolutionCodes(tq, paths); err != nil {
		return err
	}
	mlog.Printf("%d solutions downloaded", len(paths))
	return nil
}

func cleanCmd(_ chan<- task) error {
	cp := makePath()
	if err := os.RemoveAll(cp); err != nil {
		return err
	}
	mlog.Printf("%s removed", cp)
	return nil
}

// copyFile copies only a regular file.
func copyFile(srcPath, destPath string) error {
	// check file type
	fi, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeType != 0 {
		return errors.New("not a regular file")
	}
	// copy data
	bs, err := ioutil.ReadFile(srcPath)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(destPath, bs, fi.Mode())
}

// copyFiles copies all files from srcDir to destDir.
// All nested dirs with files are ignored.
func copyFiles(srcDir, destDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path == srcDir {
				return nil
			}
			return filepath.SkipDir
		}
		return copyFile(path, filepath.Join(destDir, filepath.Base(path)))
	})
}

func runCmd(name, dir string, arg ...string) (out string, err error) {
	cmd := exec.Command(name, arg...)
	cmd.Dir = dir

	bs, err := cmd.CombinedOutput()
	if err != nil {
		return
	}
	return string(bs), err
}

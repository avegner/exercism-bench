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
	"unicode"
)

const (
	exercismAddr = "https://exercism.io"
)

var commands = map[string]func(tq chan<- task) error{
	"total":    total,
	"download": download,
	"bench":    bench,
	"clean":    clean,
}

var (
	trackFlag       = ""
	exerciseFlag    = ""
	downloadDirFlag = "./solutions"
	debugFlag       = false
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
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s -track <name> -exercise <name> [opt-flag...] COMMAND

Commands:
  total
  	calculate total number of published solutions
  download
  	download published solutions
  test
  	test downloaded solutions
  bench
  	bench downloaded solutions
  clean
  	remove downloaded solutions

Flags:
`, filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}
	flag.StringVar(&trackFlag, "track", trackFlag, "language name")
	flag.StringVar(&exerciseFlag, "exercise", exerciseFlag, "exercise name")
	flag.StringVar(&downloadDirFlag, "download-dir", downloadDirFlag, "directory for downloaded solutions")
	flag.BoolVar(&debugFlag, "debug", debugFlag, "debug mode")
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
	if trackFlag == "" || exerciseFlag == "" {
		return errInvalidUsage
	}
	cmd, ok := commands[flag.Arg(0)]
	if !ok {
		return errInvalidUsage
	}

	// create pool of general purpose workers
	tq := make(chan task)
	defer close(tq)
	for i := runtime.GOMAXPROCS(0); i > 0; i-- {
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

func waitTasks(total uint, done <-chan struct{}) {
	for i := uint(0); i < total; i++ {
		<-done
	}
}

//nolint:gosec
func getContent(path string) (content string, url string, err error) {
	url = strings.Join([]string{exercismAddr, "tracks", trackFlag, "exercises", exerciseFlag, path}, "/")

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
	return filepath.Join(append([]string{downloadDirFlag, trackFlag, exerciseFlag}, path...)...)
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
	eind := strings.Index(content[sind:], "</div>")
	content = content[sind:sind+eind]
	tsm = make(map[string]string)

	for {
		fns := strings.Index(content, "<h3>")
		if fns == -1 {
			break
		}

		fne := strings.Index(content, "</h3>")
		cs := strings.Index(content, "package")
		ce := strings.Index(content, "</code></pre>")

		fn := content[fns+4:fne]
		code := content[cs:ce]
		tsm[fn] = normalizeCode(code)

		content = content[ce+12:]
	}

	return
}

type pathMap map[string]struct{}

func getSolutionPaths(tq chan<- task) (paths pathMap, err error) {
	mlog.Printf("--- getting solution paths for %s exercise ---", exerciseFlag)
	paths = make(pathMap)

	// get first solutions page
	solutionsPage, solutionsURL, err := getContent("solutions")
	if err != nil {
		err = fmt.Errorf("download of %s failed: %v", solutionsURL, err)
		return
	}
	// get total of solutions pages
	ui64, err := strconv.ParseUint(
		decimalNumberRE.FindString(
			solutionGroupsNumberRE.FindString(solutionsPage)), 10, 32)
	if err != nil {
		return
	}
	groupPagesTotal := uint(ui64)

	// schedule downloads
	done := make(chan struct{})
	mx := sync.Mutex{}
	for i := uint(0); i < groupPagesTotal; i++ {
		n := i
		go func() {
			tq <- func() {
				defer func() {
					done <- struct{}{}
				}()
				// get solution group page
				groupPage, groupURL, err := getContent(fmt.Sprintf("solutions?page=%d", n+1))
				if err != nil {
					mlog.Printf("download of %s failed: %v", groupURL, err)
					return
				}
				mlog.Printf("downloaded %s", groupURL)
				// get solution paths
				for _, p := range solutionPathRE.FindAllString(groupPage, -1) {
					// ignore duplicates if they appear
					mx.Lock()
					paths[p] = struct{}{}
					mx.Unlock()
				}
			}
		}()
	}

	// wait all tasks
	waitTasks(groupPagesTotal, done)
	mlog.Printf("---> found %d solutions", len(paths))
	return paths, nil
}

func getSolutionCodes(tq chan<- task, paths pathMap) error {
	mlog.Printf("--- getting solution codes for %s exercise ---", exerciseFlag)
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
		mlog.Printf("downloaded test suite %s", solutionURL)
		// store test suite
		ts, _ := extractTestSuite(solutionPage)
		tsp := filepath.Join(storePath,"test-suite")
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
	done := make(chan struct{})
	for p := range paths {
		path := p
		go func() {
			tq <- func() {
				defer func() {
					done <- struct{}{}
				}()
				// get solution page
				solutionPage, solutionURL, err := getContent(path)
				if err != nil {
					mlog.Printf("download of %s failed: %v", solutionURL, err)
					return
				}
				mlog.Printf("downloaded %s", solutionURL)
				// store solution code
				fp := makePath(uuidRE.FindString(path) + ".go")
				if err := ioutil.WriteFile(fp, []byte(extractSolutionCode(solutionPage)), 0600); err != nil {
					mlog.Printf("write of %s failed: %v", fp, err)
				}
			}
		}()
	}

	// wait all tasks
	waitTasks(uint(len(paths)), done)
	mlog.Println("---> done")
	return nil
}

func total(tq chan<- task) error {
	if exerciseFlag == "" {
		return errInvalidUsage
	}
	_, err := getSolutionPaths(tq)
	return err
}

func bench(_ chan<- task) error {
	// walk through all solutions
	solutionsPath := makePath()
	if err := filepath.Walk(solutionsPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if solutionsPath == path {
				return nil
			}
			return filepath.SkipDir
		}

		tmp, err := ioutil.TempDir("", "")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)

		if err = copyFile(path, filepath.Join(tmp, filepath.Base(path)), 0600); err != nil {
			return err
		}
		copyFiles(makePath("test-suite"), tmp, 0600)

		mlog.Println(path)
		mlog.Println(runBench(tmp))
		return nil
	}); err != nil {
		return err
	}

	// prepare bench environment for each solution

	// run bench

	// collect and store stats
	// report all stats in sorted way
	return nil
}

func download(tq chan<- task) error {
	if exerciseFlag == "" {
		return errInvalidUsage
	}
	paths, err := getSolutionPaths(tq)
	if err != nil {
		return err
	}
	return getSolutionCodes(tq, paths)
}

func clean(_ chan<- task) error {
	cp := makePath()
	if err := os.RemoveAll(cp); err != nil {
		return err
	}
	mlog.Printf("%s removed", cp)
	return nil
}

type solutions []*solution

type solution struct {
	user   string
	uuid   string
	ok     bool
	benchs map[string]*benchStats
}

type benchStats struct {
	time   uint
	mem    uint
	allocs uint
	size   uint
}

// sort sorts by time (the most important), mem and allocs (the least).
/*func (sols solutions) (bench string) {

	sort.SliceStable(br.stats, func(i, j int) bool {
		lh, rh := br.stats[i], br.stats[j]
		return lh.time < rh.time || lh.mem < rh.mem || lh.allocs < rh.allocs || lh.size < rh.size
	})
}*/

// getCodeSize returns number of symbols in code w/o white spaces.
// TODO: exclude comments as well
func getCodeSize(path string) (size uint, err error) {
	bs, err := ioutil.ReadFile(path)
	if err != nil {
		return
	}

	for _, r := range string(bs) {
		if unicode.IsSpace(r) {
			continue
		}
		size++
	}
	return size, nil
}

func copyFile(src, dest string, perm os.FileMode) error {
	bs, err := ioutil.ReadFile(src)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(dest, bs, perm)
}

// copyFiles copies all files from srcDir to destDir.
// All nested dirs with files are ignored.
func copyFiles(srcDir, destDir string, perm os.FileMode) error {
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

		copyFile(path, filepath.Join(destDir, filepath.Base(path)), 0600)
		return nil
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

func getWorkspacePath() (path string, err error) {
	out, err := runCmd("exercism", "", "workspace")
	if err != nil {
		return
	}
	return strings.Trim(out, "\r\n"), nil
}

// TODO: extract stats
func runBench(dir string) (out string, err error) {
	return runCmd("go", dir, "test", "-bench", ".", "-benchmem")
}

# Exercism Bench
This is a CLI tool to benchmark published solutions on [exercism](https://exercism.io/) for Go.  
It allows to:
* get a total number of published solutions for a given exercise
* download all solutions and test suite (tests and benchmarks)
* run `go test -bench . -benchmem` for each solution (tests + benchmarks)
* collect time, mem, allocs, throughput and code size (symbols except comments and whitespaces) stats
* sort benchmarking results by time for each benchmark
* implement additional or missing benchmarks
* learn from others and improve your algorithms

# How to Install
To install `exercism-bench` binary on any OS supported by Go toolchain just run:
```
go get github.com/avegner/exercism-bench
```

# How to Use
Usage is very simple and clear:
```
Usage: exercism-bench [flag...] <exercise-name> <command>

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
  -c	enable concurrency
  -d string
    	directory to store solutions (default "./solutions")
  -mp int
    	GOMAXPROCS value to set (default 4)
```

Concurrency flag allows a command to run faster in several threads (up to `GOMAXPROCS`).  
It's not recommended to enable concurrency for `bench` command if more accurate time stats are needed.

Typical use-case would be:
* ```exercism-bench -c transpose total```
* ```exercism-bench -c transpose download```
* ```exercism-bench transpose bench```
* results analysis and learning from others
* implementation of additional or missing tests and benchmarks (in ```<solutions-dir>/go/<exercise>/test-suite``` directory)
* ```exercism-bench transpose bench```
* ```exercism-bench transpose clean```

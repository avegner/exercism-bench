# Exercism Bench
This is a useful tool to benchmark published [exercism](https://exercism.io/) go solutions.  
It allows to:
* get a total number of published solutions for a given exercise
* download all solutions and test suite (tests and benchmarks)
* run `go test -bench . -benchmem` for each solution
* collect time, mem, allocs, throughput and code size (symbols except comments and whitespaces) stats
* compare benchmarking results for each benchmark
* implement additional or missing benchmarks
* learn from others and improve your algorithms

# How to Install
`
go get github.com/avegner/exercism-bench
`

# Usage
`
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
`

# Exercism Bench
This is a useful tool to benchmark published [exercism](https://exercism.io/) go solutions.  
It allows to:
* get a total number of published solutions for a given exercise
* download all solutions and test suite (tests and benchmarks)
* run `go test -bench . -benchmem` for each solution
* compare benchmarking results for each benchmark
* implement additional or missing benchmarks
* learn from others and improve your algorithms

# How to Install
`
go get github.com/avegner/exercism-bench
`

# Usage
`
Usage: exercism-bench -exercise=<name> [opt-flag...] COMMAND

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
  -concurrency
    	enable concurrency (default true)
  -download-dir string
    	directory for downloaded solutions (default "./solutions")
  -exercise string
    	exercise name
  -threads int
    	number of threads (default 4)
`

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	"sewik/pkg/dom"
	"sewik/pkg/dom/stats"
	"sewik/pkg/sewik"
	"sewik/pkg/sys"
)

var cpuFile = flag.String("profile.cpu", "", "write cpu profile to `file`")
var memFile = flag.String("profile.mem", "", "write memory profile to `file`")
var workNum = flag.Int("w", 8, "worker pool size")
var pipeSize = flag.Int("p", 10000, "pipe size per one worker")
var procNum = flag.Int("n", runtime.GOMAXPROCS(0), "set GOMAXPROCS = n")
var procDiv = flag.Int("d", 2, "set GOMAXPROCS /= d")
var cmd = flag.String("c", "x", "xml|json")

func main() {
	start := time.Now()
	flags()
	newProcCount := *procNum / *procDiv
	defaultProcCount := runtime.GOMAXPROCS(newProcCount)

	if *cpuFile != "" {
		f, err := os.Create(*cpuFile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	commands(*cmd, *workNum, *pipeSize)

	if *memFile != "" {
		f, err := os.Create(*memFile)
		if err != nil {
			log.Fatal("could not create memory profile: ", err)
		}
		defer f.Close()
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Fatal("could not write memory profile: ", err)
		}
	}

	log.Print(sys.Stats(start, *workNum, newProcCount, defaultProcCount))
}

func printJSON(filenames <-chan string, workerNum int, pipeSize int) {
	os.Exit(100)
	info := dom.NewInfo()
	for event := range sewik.ElementsOf("ZDARZENIE", filenames, workerNum, workerNum*(pipeSize+1)) {
		info.Add(event)
	}
	fmt.Printf("package dom\n\nvar GeneratedInfo = &%#v\n", info)
}

func printVar(filenames <-chan string, workerNum int, pipeSize int) {
	info := dom.NewInfo()
	for event := range sewik.ElementsOf("ZDARZENIE", filenames, workerNum, workerNum*(pipeSize+1)) {
		info.Add(event)
	}
	fmt.Printf("package dom\n\nvar GeneratedInfo = &%#v\n", info)
}

func printXMLStats(filenames <-chan string, workerNum int, pipeSize int) {
	elements := stats.NewElementsWithLock()
	for e := range sewik.ElementsOf("ZDARZENIE", filenames, workerNum, workerNum*(pipeSize+1)) {
		elements.Add(e)
	}
	stats.PrintXML(elements)
}

func commands(s string, workerCount int, pipeSize int) {
	filenames := sys.Filenames(flag.Args(), 500)
	switch s {
	case "x":
		printXMLStats(filenames, workerCount, pipeSize)
	case "j":
		printJSON(filenames, workerCount, pipeSize)
	case "v":
		printVar(filenames, workerCount, pipeSize)
	}
}

func flags() {
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "Usage: sewik (files... | \"glob\") [options]\nOptions:")
		flag.PrintDefaults()
		os.Exit(1)
	}
}

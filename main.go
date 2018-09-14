package main

import (
	"flag"
	"fmt"
	"go/build"
	"log"
	"os"
	"path/filepath"
	"strconv"
)

var (
	depth int
)

func init() {
	flag.IntVar(&depth, "d", 3, "Sets the maximum depth of the search. Set it to -1 for infinite depth, otherwise the max depth.")
}

// Gopath attempts to get the currently used $GOPATH/src.
func gopath() (string, error) {
	// Try to use $GOPATH by default
	if path := os.Getenv("GOPATH"); path != "" {
		return filepath.Join(path, "src"), nil
	}
	// Otherwise use the system default.
	path := filepath.Join(build.Default.GOPATH, "src")
	_, err := os.Stat(path)
	return path, err
}

func main() {
	log.SetFlags(0)

	path, err := gopath()
	if err != nil {
		log.Fatal(err)
	}

	flag.Parse()

	// If no path supplied then change directory to $GOPATH.
	if flag.NArg() == 0 {
		fmt.Print(path)
		return
	}
	// Using '^', try to go to the vendor's parent
	if flag.Arg(0) == VendorToken {
		ok, err := goToVendorParent()
		if err != nil {
			log.Fatal(err)
		}
		if ok {
			return
		}
	}
	if found := find(path); found {
		return
	}
	os.Exit(1)
}

func find(inPath string) bool {
	w := NewPkgFinder(inPath, depth)

	matches, err := w.Find(flag.Arg(0), 10)
	if err != nil {
		log.Fatal(err)
	}
	if len(matches) < 1 {
		fmt.Println("no match found")
		return false
	}
	if len(matches) == 1 {
		fmt.Println(matches[0].Target)
		return true
	}
	// If not just the package provided we assume there is a number to select
	// a package from the possible matches, outputted in a previous run.
	if flag.NArg() > 1 {
		i, err := strconv.Atoi(flag.Arg(1))
		if err != nil {
			log.Fatalf("cannot parse requested index %s: %s", flag.Arg(1), err)
		}
		max := len(matches) - 1
		if i > max {
			log.Fatalf("%d is an invalid index (max %d)", i, max)
		}
		fmt.Println(matches[i].Target)
		return true
	}
	for i, m := range matches {
		rel, _ := filepath.Rel(inPath, m.Target)
		log.Printf("  %d %s", i, rel)
	}
	return true
}

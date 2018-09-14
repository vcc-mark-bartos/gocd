package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kr/fs"
	"github.com/renstrom/fuzzysearch/fuzzy"
)

// OrderedRanks contains paths with a `Distance` field denoting how far they are
// from a base path.
type OrderedRanks []fuzzy.Rank

func (r *OrderedRanks) rank(rel string, by string) {
	for _, elem := range *r {
		entry, _ := filepath.Rel(rel, elem.Target)
		path := append(strings.Split(entry, string(filepath.Separator)), entry)
		ranks := fuzzy.RankFindFold(by, path)
		min := ranks[0].Distance
		for _, r := range ranks {
			if r.Distance < min {
				min = r.Distance
			}
		}
		elem.Distance = min
	}
}

func (r OrderedRanks) Len() int {
	return len(r)
}

func (r OrderedRanks) Swap(i, j int) {
	r[i], r[j] = r[j], r[i]
}

func (r OrderedRanks) Less(i, j int) bool {
	if r[i].Distance < r[j].Distance {
		return true
	}
	if r[i].Distance > r[j].Distance {
		return false
	}
	return r[i].Target < r[j].Target
}

// PkgFinder finds a Go package.
type PkgFinder struct {
	// gopath points to $GOPATH/src.
	gopath string

	// cache caches the folder contents for faster lookups.
	cache *cache

	// depthLimit sets the maximum depth of the search.
	// Set it to -1 for infinite depth, otherwise the max depth.
	depthLimit int
}

// NewPkgFinder creates a new `PkgFinder` relative to `path`.
func NewPkgFinder(path string, depth int) *PkgFinder {
	return &PkgFinder{
		gopath:     path,
		cache:      newCache(os.ExpandEnv(CacheFile)),
		depthLimit: depth,
	}
}

// matchComponent scans every component of the relative path until it finds a
// match.
func (w *PkgFinder) matchComponent(find string, components *[]string) (string, bool) {
	for x := 0; x < len(*components); x++ {
		path := filepath.Join((*components)[x:]...)
		if find == path {
			return path, true
		}
	}
	return "", false
}

func (w *PkgFinder) walkPath(walker *fs.Walker, find string, pkg string, components *[]string, matches *[]string) {
	stat := walker.Stat()
	if w.cache.fullScan {
		mtime := stat.ModTime().Unix()
		w.cache.add(pkg, mtime)
	} else {
		mtime := stat.ModTime().Unix()
		prevMtime, inCache := w.cache.get(pkg)
		if !inCache {
			w.cache.changed = true
			w.cache.add(pkg, mtime)
		}
		if _, found := w.matchComponent(find, components); found {
			in := sort.SearchStrings(*matches, pkg)
			if len(*matches) <= in {
				*matches = append(*matches, pkg)
			}
			return
		}
		// Due to how inodes work (the current inode's mtime only changes if a
		// direct child is modified, it remains unchanged for grandchild and so
		// on) we can only use mtime to skip the last level
		if w.depthLimit-1 == depth {
			if inCache && mtime <= prevMtime {
				walker.SkipDir()
			}
		}
	}
}

func (w *PkgFinder) walker(root string, find string) []string {
	prevCache := make([]string, 0)
	walker := fs.Walk(root)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		stat := walker.Stat()
		isDir := stat.IsDir()
		path := walker.Path()

		if path == w.gopath {
			continue
		}
		// pkg is the package path, so we only use its parent dir if it is a file
		// otherwise we through off the depth calculation
		var pkg string
		if isDir {
			pkg, _ = filepath.Rel(w.gopath, path)
		} else {
			pkg, _ = filepath.Rel(w.gopath, filepath.Dir(path))
		}

		//   `root` |  0  |  1  | 2
		// ........./$repo/$user/$pkg
		components := strings.Split(pkg, string(filepath.Separator))
		depth := len(components)

		depthLimitHit := w.depthLimit > -1 && depth >= w.depthLimit
		if depthLimitHit {
			walker.SkipDir()
		}
		if !isDir { // we only care about folders
			continue
		}
		// Skip if path contains .git or vendor
		if isDir &&
			(strings.HasPrefix(stat.Name(), ".") ||
				strings.HasPrefix(stat.Name(), "_") ||
				strings.Contains(path, "vendor")) {
			walker.SkipDir()
			continue
		}
		w.walkPath(walker, find, pkg, &components, &prevCache)
	}
	return prevCache
}

func (w *PkgFinder) isValid(find string) (OrderedRanks, bool) {
	// If absolute path then go straight to it
	if path.IsAbs(find) {
		return OrderedRanks{
			{
				Target: find,
			},
		}, true
	}
	// If path is a real path relative to gopath then use it
	abs := filepath.Join(w.gopath, find)
	_, err := os.Stat(abs)
	if err == nil {
		return OrderedRanks{
			{
				Target: abs,
			},
		}, true
	}
	return nil, false
}

func (w *PkgFinder) findInCache(find string) (OrderedRanks, bool) {
	paths, fullMatch := w.cache.contains(find, 1)
	if fullMatch {
		if w.cache.fullScan {
			return OrderedRanks{
				{
					Target: filepath.Join(w.gopath, paths[0]),
				},
			}, true
		}
	}
	valids := w.recheckPaths(paths)
	if len(valids) > 0 {
		return valids, fullMatch
	}
	return nil, false
}

func (w *PkgFinder) traverse(find string) OrderedRanks {
	pkgs := w.walker(w.gopath, find)
	pkgsLen := len(pkgs)
	if pkgs != nil && pkgsLen > 0 {
		if pkgsLen > 1 {
			found := make(OrderedRanks, len(pkgs))
			for i, r := range pkgs {
				found[i] = fuzzy.Rank{
					Target: filepath.Join(w.gopath, r),
				}
			}
			return found
		}
		return OrderedRanks{
			{
				Target: filepath.Join(w.gopath, pkgs[0]),
			},
		}
	}
	return nil
}

// Find a package by the given key.
func (w *PkgFinder) Find(find string, max int) (OrderedRanks, error) {
	if ret, found := w.isValid(find); found {
		return ret, nil
	}
	defer func() {
		if err := w.cache.save(); err != nil {
			fmt.Fprintln(os.Stderr, "error during cache saving:", err)
		}
	}()
	if w.cache.fullScan {
		_ = w.walker(w.gopath, "")
		w.cache.changed = true
		w.cache.fullScan = false
	}
	// Try cache first for a full match only
	cached, fullMatch := w.findInCache(find)
	if cached != nil && fullMatch {
		return cached, nil
	}
	// Otherwise traverse the dir tree to avoid the cache never being updated
	possibleMatches := w.traverse(find)

	if len(possibleMatches) > 1 {
		possibleMatches.rank(w.gopath, find)
		sort.Sort(possibleMatches)
		return possibleMatches, nil
	}
	// We could not find any matches, so try a fuzzy search of all the cached
	// paths (at this point the whole cache has been updated)
	fuzzyMatches := w.fuzzyFindMatches(find, 10)

	return fuzzyMatches, nil
}

func (w *PkgFinder) recheckPaths(paths []string) OrderedRanks {
	ret := make(OrderedRanks, 0, len(paths))
	for _, path := range paths {
		var err error
		abs := filepath.Join(w.gopath, path)
		// If we just did a fullScan, cache is valid so no need to check
		if !w.cache.fullScan {
			_, err = os.Stat(abs)
		}
		if err == nil {
			ret = append(ret, fuzzy.Rank{
				Target: abs,
			})
		} else { // path is not valid anymore
			w.cache.del(path)
		}
	}
	return ret
}

func (w *PkgFinder) fuzzyFindMatches(find string, num int) OrderedRanks {
	matches := make(map[string]fuzzy.Rank)

	for entry := range w.cache.storage {
		path := append(strings.Split(entry, string(filepath.Separator)), entry)
		ranks := fuzzy.RankFindFold(find, path)

		for _, r := range ranks {
			if r.Distance > 10 {
				continue
			}
			m, ok := matches[entry]

			if (ok && r.Distance < m.Distance) || !ok {
				r.Target = filepath.Join(w.gopath, entry)
				matches[entry] = r
			}
		}
	}
	ret := make(OrderedRanks, len(matches))
	if len(matches) == 0 {
		return ret
	}
	var i int
	for _, r := range matches {
		ret[i] = r
		i++
	}
	sort.Sort(ret)
	if len(ret) > num {
		ret = ret[:num]
	}
	return ret
}

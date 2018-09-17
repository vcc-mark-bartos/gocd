package main

import (
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kr/fs"
	"github.com/renstrom/fuzzysearch/fuzzy"
)

type OrderedRanks []fuzzy.Rank

func (r *OrderedRanks) rank(rel string, by string) {
	for _, elem := range *r {
		if elem.Target == "" {
			continue
		}
		currentPath := append(strings.Split(elem.Target, string(filepath.Separator)), elem.Target)
		ranks := fuzzy.RankFindFold(by, currentPath)
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

type PkgFinder struct {
	goPath     string
	cache      *cache
	depthLimit int
	max        int
}

func NewPkgFinder(path string, depth int, max int) *PkgFinder {
	return &PkgFinder{
		goPath:     path,
		cache:      newCache(os.ExpandEnv(CacheFile)),
		depthLimit: depth,
		max:        max,
	}
}

func (w *PkgFinder) Find(find string) OrderedRanks {
	if ret := w.findRealPath(find); ret != nil {
		return ret
	}
	defer func() {
		if err := w.cache.save(); err != nil {
			log.Println("error during cache saving:", err)
		}
	}()

	if match := w.findExact(find); match != nil {
		return match
	}

	if match := w.walker(w.goPath, find); match != nil {
		return match
	}

	return w.fuzzyFindMatches(find)
}

func (w *PkgFinder) findExact(find string) OrderedRanks {
	var ret OrderedRanks
	for entry := range w.cache.storage {
		components := strings.Split(entry, string(filepath.Separator))
		if p := w.matchExactComponent(find, components); p != "" {
			p := filepath.Join(w.goPath, entry)
			if _, err := os.Stat(p); err != nil {
				w.cache.del(entry)
				return nil
			}
			ret = append(ret, fuzzy.Rank{Target: entry})
		}
	}
	ret.rank(w.goPath, find)
	return ret
}

func (w *PkgFinder) walker(root string, find string) OrderedRanks {
	var matches OrderedRanks
	var prevCache []string
	walker := fs.Walk(root)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			log.Println(err)
			continue
		}
		currentDir := walker.Path()
		if currentDir == w.goPath {
			continue
		}
		stat := walker.Stat()
		if !stat.IsDir() {
			continue
		}

		pkg, _ := filepath.Rel(w.goPath, currentDir)
		components := strings.Split(pkg, string(filepath.Separator))

		if w.depthLimit > -1 && len(components) >= w.depthLimit {
			walker.SkipDir()
		}

		if strings.HasPrefix(stat.Name(), ".") ||
			strings.HasPrefix(stat.Name(), "_") ||
			strings.Contains(currentDir, "vendor") {
			walker.SkipDir()
			continue
		}

		cached := w.cache.add(pkg, stat.ModTime())

		if p := w.matchExactComponent(find, components); p != "" {
			in := sort.SearchStrings(prevCache, pkg)
			if len(prevCache) <= in {
				matches = append(matches, fuzzy.Rank{Target: find})
			}
		}

		// Due to how inodes work (the current inode's mtime only changes if a
		// direct child is modified, it remains unchanged for grandchild and so
		// on) we can only use mtime to skip the last level
		if w.depthLimit-1 == depth && !cached {
			walker.SkipDir()
		}
	}
	matches.rank(w.goPath, find)
	return matches
}

func (w *PkgFinder) matchExactComponent(find string, components []string) string {
	for x := 0; x < len(components); x++ {
		p := filepath.Join((components)[x:]...)
		if find == p {
			return p
		}
	}
	return ""
}

func (w *PkgFinder) findRealPath(find string) OrderedRanks {
	// If absolute path then go straight to it
	if path.IsAbs(find) {
		return OrderedRanks{
			{
				Target: find,
			},
		}
	}
	// If path is a real path relative to goPath then use it
	abs := filepath.Join(w.goPath, find)
	_, err := os.Stat(abs)
	if err == nil {
		return OrderedRanks{
			{
				Target: abs,
			},
		}
	}
	return nil
}

func (w *PkgFinder) fuzzyFindMatches(find string) OrderedRanks {
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
				r.Target = entry
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
	if len(ret) > w.max {
		ret = ret[:w.max]
	}
	return ret
}

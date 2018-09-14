package main

import (
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// CacheFile is the file used for caching the folder structure.
	CacheFile = "$HOME/.cache/gocd/cache"

	// PrevsFile is the file used for caching the at most 10 folders when that
	// 'fuzzy' matched the queried package.
	PrevsFile = "$HOME/.cache/gocd/prevs"
)

// cache caches the contents of a directory for faster lookups.
type cache struct {
	file    string
	storage map[string]int64

	// changed is set when the folder structure has changed compared to the cache
	changed  bool
	fullScan bool
}

// Creates and loads (according to `file`) a new `cache`.
func newCache(file string) *cache {
	c := &cache{
		file:    file,
		storage: make(map[string]int64),
	}
	c.loadCache()
	return c
}

// load loads the cache file. If it does not exists it creates it.
func (c *cache) loadCache() {
	if _, err := os.Stat(c.file); err != nil {
		c.changed = true
		c.fullScan = true
		return
	}
	if err := c.deserialize(c.file); err != nil {
		c.loadCacheFail(err)
		return
	}
}

func (c *cache) loadCacheFail(err error) {
	fmt.Fprintln(os.Stderr, err)
	c.changed = true
	c.fullScan = true
}

// save saves the directory structure into the cache file.
func (c *cache) save() error {
	if !c.changed && !c.fullScan {
		return nil
	}
	err := os.MkdirAll(filepath.Dir(c.file), os.ModePerm)
	if err != nil {
		return err
	}
	if err := c.serialize(c.file); err != nil {
		return err
	}
	return nil
}

func (c *cache) add(path string, mtime int64) {
	c.storage[path] = mtime
	c.changed = true
}

func (c *cache) get(path string) (int64, bool) {
	v, ok := c.storage[path]
	return v, ok
}

func (c *cache) del(path string) {
	if _, ok := c.get(path); !ok {
		return
	}
	c.changed = true
	delete(c.storage, path)
}

// contains returns a slice of matches, at most `max`. If the second return
// value is true, it is a full match, that is not only a part of the path's
// components matched, thus in that case the return slice has length 1, the
// matching path.
func (c *cache) contains(path string, max int) ([]string, bool) {
	_, found := c.get(path)
	if found {
		return []string{path}, true
	}
	ret := make([]string, 0, max)
	for entry := range c.storage {
		components := strings.Split(entry, string(filepath.Separator))
		for x := len(components) - 1; x >= 0; x-- {
			p := filepath.Join(components[x:]...)
			if path == p {
				ret = append(ret, entry)
				if len(ret) > max {
					return ret, false
				}
			}
		}

	}
	return ret, false
}

func (c *cache) deserialize(path string) error {
	file, err := os.OpenFile(path, os.O_RDONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	dec := gob.NewDecoder(file)
	if err := dec.Decode(&c.storage); err != nil {
		return err
	}
	return nil
}

func (c *cache) serialize(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	enc := gob.NewEncoder(file)
	if err = enc.Encode(c.storage); err != nil {
		return err
	}
	return nil
}
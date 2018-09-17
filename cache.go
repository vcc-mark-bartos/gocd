package main

import (
	"encoding/gob"
	"log"
	"os"
	"path/filepath"
	"time"
)

const (
	// CacheFile is the file used for caching the folder structure.
	CacheFile = "$HOME/.cache/gocd/cache"
)

// cache caches the contents of a directory for faster lookups.
type cache struct {
	file    string
	storage map[string]time.Time

	// changed is set when the folder structure has changed compared to the cache
	changed bool
}

// Creates and loads (according to `file`) a new `cache`.
func newCache(file string) *cache {
	c := &cache{
		file:    file,
		storage: make(map[string]time.Time),
	}
	c.loadCache()
	return c
}

// load loads the cache file. If it does not exists it creates it.
func (c *cache) loadCache() {
	if _, err := os.Stat(c.file); err != nil {
		log.Println(err)
		c.changed = true
		return
	}
	if err := c.deserialize(c.file); err != nil {
		log.Println(err)
		c.changed = true
		return
	}
}

// save saves the directory structure into the cache file.
func (c *cache) save() error {
	if !c.changed {
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

func (c *cache) add(path string, mTime time.Time) bool {
	if t, ok := c.get(path); ok {
		if t.Equal(mTime) {
			return false
		}
	}
	c.changed = true
	c.storage[path] = mTime
	return true
}

func (c *cache) get(path string) (time.Time, bool) {
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

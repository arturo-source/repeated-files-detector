package main

import (
	"crypto/md5"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type fHashed struct {
	err  error
	path string
	hash [md5.Size]byte
}

// allFilesRecursively receives a directory and returns a channel where all files (lower than 1GB) found are sent
func allFilesRecursively(done <-chan struct{}, directory string) (<-chan string, <-chan error) {
	const GB = 1 << 30
	paths := make(chan string)
	errc := make(chan error, 1)

	go func() {
		defer close(paths)

		errc <- filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if !info.Mode().IsRegular() {
				return nil
			}

			if info.Size() > GB {
				fmt.Fprintf(os.Stderr, "%s is bigger than 1GB (~%dGB). Will skip it.\n", path, info.Size()/GB)
				return nil
			}

			// Abort the walk if done is closed.
			select {
			case paths <- path:
				return nil
			case <-done:
				return errors.New("walk canceled")
			}
		})
	}()

	return paths, errc
}

// createHashes receives `paths` channel that will be iterated, and `c` channel where will send file hashed
func createHashes(done <-chan struct{}, paths <-chan string, c chan<- fHashed) {
	for path := range paths {
		data, err := os.ReadFile(path)
		select {
		case c <- fHashed{err, path, md5.Sum(data)}:
		case <-done:
			return
		}
	}
}

// MD5AllFiles executes concurrently:
//
// - Find all files recursively
// - Calculate hash from each file
// - ❌ Compare file hashes
func MD5AllFiles(directory string, out io.Writer, n int) error {
	// Necessary to terminate file hashing if the process is interrupted
	done := make(chan struct{})
	defer close(done)

	paths, errc := allFilesRecursively(done, directory)

	// Add n goroutines hashing files
	var wg sync.WaitGroup
	wg.Add(n)

	hashes := make(chan fHashed)
	go func() {
		wg.Wait()
		close(hashes)
	}()

	for i := 0; i < n; i++ {
		go func() {
			createHashes(done, paths, hashes)
			wg.Done()
		}()
	}

	// Write file hashes
	for fh := range hashes {
		if fh.err != nil {
			return fh.err
		}

		fmt.Fprintf(out, "%s %x\n", fh.path, fh.hash)
	}

	// be quiet!!
	// if you place this code under `paths, errc := allFilesRecursively(done, directory)` it will produce a deadlock!
	// because it will be locked reading `err := <-errc` and writing case paths <- path:
	if err := <-errc; err != nil {
		return err
	}

	return nil
}

func run() error {
	var directory, outputFile string
	var nGoroutines int
	flag.StringVar(&directory, "d", "", "Directory to evaluate")
	flag.StringVar(&outputFile, "o", "", "Name of the file to output the results (default output is stdout)")
	flag.IntVar(&nGoroutines, "threads", 8, "Number of goroutines running at the same time")

	flag.Parse()

	if directory == "" {
		flag.Usage()
		return errors.New("directory must be provided using -d option")
	}

	// use stdout if no file is chosen
	out := os.Stdout
	if outputFile != "" {
		f, err := os.Create(outputFile)
		if err != nil {
			return err
		}
		defer f.Close()

		out = f
	}

	return MD5AllFiles(directory, out, nGoroutines)
}

func main() {
	if err := run(); err != nil {
		fmt.Printf("error: %s\n", err)
	}
}

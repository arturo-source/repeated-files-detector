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

func allFilesRecursively(done <-chan struct{}, directory string) (<-chan string, <-chan error) {
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

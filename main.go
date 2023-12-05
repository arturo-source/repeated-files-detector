package main

import (
	"crypto/md5"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type fHashed struct {
	err       error
	path      string
	directory string
	hash      [md5.Size]byte
}

// getFilesRecursively receives a directory and returns a channel where all files (lower than 1GB) found are sent
func getFilesRecursively(done <-chan struct{}, directory string) (<-chan string, <-chan error) {
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

// executeGoroutines adds n goroutines and wait for them
func executeGoroutines(mainFunc, closeFunc func(), n int) {
	var wg sync.WaitGroup
	wg.Add(n)

	go func() {
		wg.Wait()
		closeFunc()
	}()

	for i := 0; i < n; i++ {
		go func() {
			mainFunc()
			wg.Done()
		}()
	}
}

// md5All calculates hash from each file concurrently
func md5All(done <-chan struct{}, paths <-chan string, n int) <-chan fHashed {
	hashes := make(chan fHashed)

	mainFunc := func() {
		// send hash from each path read
		for path := range paths {
			data, err := os.ReadFile(path)
			select {
			case hashes <- fHashed{
				err:       err,
				directory: filepath.Dir(path),
				path:      path,
				hash:      md5.Sum(data),
			}:
			case <-done:
				return
			}
		}
	}
	closeFunc := func() {
		close(hashes)
	}

	executeGoroutines(mainFunc, closeFunc, n)
	return hashes
}

// groupByDirectory groups from hashes channel using directory as map key
func groupByDirectory(hashes <-chan fHashed) (map[string][]*fHashed, error) {
	sameDir := map[string][]*fHashed{}

	for fh := range hashes {
		if fh.err != nil {
			return sameDir, fh.err
		}

		sameDir[fh.directory] = append(sameDir[fh.directory], &fh)
	}

	return sameDir, nil
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

	// Necessary to terminate goroutines if the process is interrupted
	done := make(chan struct{})
	defer close(done)

	paths, errc := getFilesRecursively(done, directory)
	hashesc := md5All(done, paths, nGoroutines)
	filesByDirectory, err := groupByDirectory(hashesc)
	if err != nil {
		return err
	}

	for dir, files := range filesByDirectory {
		if len(files) > 1 {
			fmt.Fprintf(out, "%s has %d files\n", dir, len(files))
		}
	}

	// be quiet!!
	// if you place `<-errc` (channel reading) before `paths<-` (other channel writing) it will produce a deadlock!
	return <-errc
}

func main() {
	if err := run(); err != nil {
		fmt.Printf("error: %s\n", err)
	}
}

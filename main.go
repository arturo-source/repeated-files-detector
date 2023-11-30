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

// md5All calculates hash from each file concurrently
func md5All(done <-chan struct{}, paths <-chan string, n int) <-chan fHashed {
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
			// send hash from each path read
			for path := range paths {
				data, err := os.ReadFile(path)
				select {
				case hashes <- fHashed{err, path, md5.Sum(data)}:
				case <-done:
					return
				}
			}

			wg.Done()
		}()
	}

	return hashes
}

type fRepeated struct {
	mu       sync.Mutex
	repeated map[[md5.Size]byte][]string
}

// append receives a fHashed and appends securely to repeated map (avoid race conditions)
func (files *fRepeated) append(fh fHashed) {
	files.mu.Lock()
	defer files.mu.Unlock()
	files.repeated[fh.hash] = append(files.repeated[fh.hash], fh.path)
}

// findRepeatedHashes writes the repeated file paths from hashes channel
func findRepeatedHashes(out io.Writer, hashes <-chan fHashed) error {
	fRep := fRepeated{
		repeated: make(map[[md5.Size]byte][]string),
	}

	for fh := range hashes {
		if fh.err != nil {
			return fh.err
		}

		fRep.append(fh)
	}

	for hash, files := range fRep.repeated {
		if len(files) > 1 {
			fmt.Fprintf(out, "%x (%d times) %s\n", hash, len(files), files)
		}
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

	// Necessary to terminate goroutines if the process is interrupted
	done := make(chan struct{})
	defer close(done)

	paths, errc := allFilesRecursively(done, directory)
	hashesc := md5All(done, paths, nGoroutines)
	err := findRepeatedHashes(out, hashesc)
	if err != nil {
		return err
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

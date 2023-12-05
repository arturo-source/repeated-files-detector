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
func groupByDirectory(hashes <-chan fHashed) (map[string][]fHashed, error) {
	sameDir := map[string][]fHashed{}

	for fh := range hashes {
		if fh.err != nil {
			return sameDir, fh.err
		}

		sameDir[fh.directory] = append(sameDir[fh.directory], fh)
	}

	return sameDir, nil
}

type fRepeated struct {
	dir1  []fHashed
	dir2  []fHashed
	count uint
}

// countRepeatedFiles counts how many files are repeated between all 2 different directories
func countRepeatedFiles(done <-chan struct{}, filesByDirectory map[string][]fHashed, n int) <-chan fRepeated {
	repeatedZero := make(chan fRepeated)
	repeatedCounted := make(chan fRepeated)

	mainFunc := func() {
		for fr := range repeatedZero {
			// compare both directories file hashes
			for _, fh1 := range fr.dir1 {
				for _, fh2 := range fr.dir2 {
					if fh1.hash == fh2.hash {
						fr.count++
					}
				}
			}

			select {
			case repeatedCounted <- fr:
			case <-done:
				return
			}
		}
	}
	closeFunc := func() {
		close(repeatedCounted)
	}

	executeGoroutines(mainFunc, closeFunc, n)

	go func() {
		// extract directories to use them as keys in the map
		directories := make([]string, 0, len(filesByDirectory))
		for dir := range filesByDirectory {
			directories = append(directories, dir)
		}

		// send dir1 to compare with all dir2 (avoid comparing itself and the directories compared before)
		for i, dir1 := range directories {
			for _, dir2 := range directories[i+1:] {
				repeatedZero <- fRepeated{filesByDirectory[dir1], filesByDirectory[dir2], 0}
			}
		}

		close(repeatedZero)
	}()

	return repeatedCounted
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

	repeatedc := countRepeatedFiles(done, filesByDirectory, nGoroutines)
	for fr := range repeatedc {
		if fr.count > 0 {
			fmt.Fprintf(out, "%s and %s have %d repeated files\n", fr.dir1[0].directory, fr.dir2[0].directory, fr.count)
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

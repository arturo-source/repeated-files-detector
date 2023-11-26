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
	err  error
	path string
	hash [md5.Size]byte
}

func MD5AllFiles(directory string) (<-chan fHashed, <-chan error) {
	c := make(chan fHashed)
	errc := make(chan error, 1)

	go func() {
		var wg sync.WaitGroup

		errc <- filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if !info.Mode().IsRegular() {
				return nil
			}

			wg.Add(1)
			go func() {
				data, err := os.ReadFile(path)
				c <- fHashed{err, path, md5.Sum(data)}
				wg.Done()
			}()

			return nil
		})

		go func() {
			wg.Wait()
			close(c)
		}()
	}()

	return c, errc
}

func run() error {
	var directory, outputFile string
	flag.StringVar(&directory, "d", "", "Directory to evaluate")
	flag.StringVar(&outputFile, "o", "", "Name of the file to output the results (default output is stdout)")

	flag.Parse()

	if directory == "" {
		flag.Usage()
		return errors.New("directory must be provided using -d option")
	}

	hashes, errc := MD5AllFiles(directory)
	if err := <-errc; err != nil {
		return err
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

	for fh := range hashes {
		if fh.err != nil {
			return fh.err
		}

		fmt.Fprintf(out, "%s %x\n", fh.path, fh.hash)
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Printf("error: %s\n", err)
	}
}

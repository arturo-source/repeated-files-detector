package main

import (
	"crypto/md5"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func MD5All(directory string) (map[string][md5.Size]byte, error) {
	m := make(map[string][md5.Size]byte)
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		m[path] = md5.Sum(data)
		return nil

	})

	return m, err
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

	hashes, err := MD5All(directory)
	if err != nil {
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

	for k, v := range hashes {
		fmt.Fprintf(out, "%s %x\n", k, v)
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Printf("error: %s\n", err)
	}
}

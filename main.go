package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

func todo(directory string) (string, error) {
	// todo
	return "", nil
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

	result, err := todo(directory)
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

	_, err = out.Write([]byte(result))
	return err
}

func main() {
	if err := run(); err != nil {
		fmt.Printf("error: %s\n", err)
	}
}

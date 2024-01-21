package main

import (
	"crypto/md5"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var (
	units      = map[string]int64{}
	validUnits = []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
)

func init() {
	for i, unit := range validUnits {
		units[unit] = 1 << (10 * i)
	}
}

func parseToBytes(size string) (int64, error) {
	isNumber := func(num rune) bool {
		return num >= '0' && num <= '9'
	}

	var number string
	for _, num := range size {
		if !isNumber(num) {
			break
		}

		number += string(num)
	}

	if len(number) == 0 {
		return 0, fmt.Errorf("%s at least, first character should be a number", size)
	}

	numberInt, err := strconv.Atoi(number)
	if err != nil {
		return 0, err
	}

	unit := size[len(number):]
	unit = strings.ToUpper(unit)
	if _, exist := units[unit]; !exist {
		return 0, fmt.Errorf("%s is not a valid unit %s", unit, validUnits)
	}

	return int64(numberInt) * units[unit], nil
}

type fHashed struct {
	err       error
	path      string
	directory string
	hash      [md5.Size]byte
}

// getFilesRecursively receives a directory and returns a channel where all files found are sent
func getFilesRecursively(done <-chan struct{}, directory string, reg *regexp.Regexp, minSize, maxSize int64) (<-chan string, <-chan error) {
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

			if reg != nil && reg.MatchString(path) {
				return nil
			}

			fileSize := info.Size()
			if fileSize < minSize || fileSize > maxSize {
				fmt.Fprintf(os.Stderr, "%s (%dB) is out of bounds (%dB - %dB). Will skip it.\n", path, fileSize, minSize, maxSize)
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

type fToCompare struct {
	files1 []fHashed
	files2 []fHashed
}

// getDirectoriesToCompare makes all the combinations of two directories
func getDirectoriesToCompare(done <-chan struct{}, filesByDirectory map[string][]fHashed) <-chan fToCompare {
	repeated := make(chan fToCompare)

	go func() {
		defer close(repeated)

		// extract directories to use them as keys in the map
		directories := make([]string, 0, len(filesByDirectory))
		for dir := range filesByDirectory {
			directories = append(directories, dir)
		}

		// send dir1 to compare with all dir2 (avoid comparing itself and the directories compared before)
		for i, dir1 := range directories {
			for _, dir2 := range directories[i+1:] {
				select {
				case repeated <- fToCompare{filesByDirectory[dir1], filesByDirectory[dir2]}:
				case <-done:
					return
				}

			}
		}
	}()

	return repeated
}

type fRepeated struct {
	f1 fHashed
	f2 fHashed
}

// countRepeatedFiles counts how many files are repeated between 2 different directories
func countRepeatedFiles(done <-chan struct{}, f2c <-chan fToCompare, n int) [][]fRepeated {
	repeatedMatrix := [][]fRepeated{}
	repeatedArray := make(chan []fRepeated)

	mainFunc := func() {
		for c := range f2c {
			// compare both directories file hashes
			repeatedFiles := []fRepeated{}
			for _, fh1 := range c.files1 {
				for _, fh2 := range c.files2 {
					if fh1.hash == fh2.hash {
						repeatedFiles = append(repeatedFiles, fRepeated{fh1, fh2})
					}
				}
			}

			select {
			case repeatedArray <- repeatedFiles:
			case <-done:
				return
			}
		}
	}
	closeFunc := func() {
		close(repeatedArray)
	}

	executeGoroutines(mainFunc, closeFunc, n)

	for arr := range repeatedArray {
		repeatedMatrix = append(repeatedMatrix, arr)
	}

	return repeatedMatrix
}

// printRepeatedFiles writes each folder (with more than n repeated files) into out writer
func printRepeatedFiles(out io.Writer, repeatedMatrix [][]fRepeated, n int) {
	biggestFilenameSize := func(bigestSize1, bigestSize2 int, frs []fRepeated) (int, int) {
		for _, fr := range frs {
			filename1, filename2 := filepath.Base(fr.f1.path), filepath.Base(fr.f2.path)

			if len(filename1) > bigestSize1 {
				bigestSize1 = len(filename1)
			}
			if len(filename2) > bigestSize2 {
				bigestSize2 = len(filename2)
			}
		}

		return bigestSize1, bigestSize2
	}

	printDividingLine := func(size1, size2 int) {
		fmt.Fprintf(out, "+%s+\n", strings.Repeat("-", size1+size2+4))
	}
	printTwoFiles := func(size1, size2 int, path1, path2 string) {
		fmt.Fprintf(out, "|%*s == %-*s|\n", size1, path1, size2, path2)
	}

	for _, repeatedArray := range repeatedMatrix {
		if len(repeatedArray) < n {
			continue
		}

		dir1, dir2 := repeatedArray[0].f1.directory, repeatedArray[0].f2.directory
		size1, size2 := biggestFilenameSize(len(dir1), len(dir2), repeatedArray)

		printDividingLine(size1, size2)
		printTwoFiles(size1, size2, dir1, dir2)
		printDividingLine(size1, size2)

		for _, repeated := range repeatedArray {
			filename1, filename2 := filepath.Base(repeated.f1.path), filepath.Base(repeated.f2.path)
			printTwoFiles(size1, size2, filename1, filename2)
		}

		printDividingLine(size1, size2)
	}
}

func run() error {
	var directory, outputFile, avoidDirs string
	var nGoroutines, nRepeated int
	var minStr, maxStr string
	flag.StringVar(&directory, "directory", "", "Directory to evaluate (required)")
	flag.StringVar(&outputFile, "output", "", "Name of the file to output the results (default output is stdout)")
	flag.StringVar(&avoidDirs, "avoid", "", "Regex used to avoid evaluating matching directories")
	flag.IntVar(&nGoroutines, "threads", 8, "Number of goroutines running at the same time")
	flag.IntVar(&nRepeated, "repeated", 1, "Minimum number of repeated files in two different directories")
	flag.StringVar(&minStr, "min", "0B", "Minimum file size to analize")
	flag.StringVar(&maxStr, "max", "1GB", "Maximum file size to analize")

	flag.Parse()

	if directory == "" {
		flag.Usage()
		return errors.New("directory must be provided")
	}

	minSize, err := parseToBytes(minStr)
	if err != nil {
		return err
	}

	maxSize, err := parseToBytes(maxStr)
	if err != nil {
		return err
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

	var reg *regexp.Regexp
	if avoidDirs != "" {
		r, err := regexp.Compile(avoidDirs)
		if err != nil {
			return err
		}

		reg = r
	}

	// Necessary to terminate goroutines if the process is interrupted
	done := make(chan struct{})
	defer close(done)

	paths, errc := getFilesRecursively(done, directory, reg, minSize, maxSize)
	hashesc := md5All(done, paths, nGoroutines)
	filesByDirectory, err := groupByDirectory(hashesc)
	if err != nil {
		return err
	}

	repeatedZero := getDirectoriesToCompare(done, filesByDirectory)
	repeatedMatrix := countRepeatedFiles(done, repeatedZero, nGoroutines)
	printRepeatedFiles(out, repeatedMatrix, nRepeated)

	// be quiet!!
	// if you place `<-errc` (channel reading) before `paths<-` (other channel writing) it will produce a deadlock!
	return <-errc
}

func main() {
	if err := run(); err != nil {
		fmt.Printf("error: %s\n", err)
	}
}

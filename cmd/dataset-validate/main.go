package main

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/01121531/HUICHUAN-AI/pkg/datasetcapture"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: dataset-validate <sample.jsonl>")
		os.Exit(2)
	}
	rows, err := validateFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("valid dataset: rows=%d file=%s\n", rows, os.Args[1])
}

func validateFile(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	row := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			row++
			if err := validateLine(line, row); err != nil {
				return row - 1, err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return row, readErr
		}
	}
	if row == 0 {
		return 0, fmt.Errorf("dataset is empty")
	}
	return row, nil
}

func validateLine(line []byte, row int) error {
	if err := datasetcapture.ValidateJSONLine(line); err != nil {
		return fmt.Errorf("row %d: %w", row, err)
	}
	return nil
}

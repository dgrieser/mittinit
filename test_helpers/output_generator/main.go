package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	lines := flag.Int("lines", 100, "Number of lines to print to stdout")
	lineLength := flag.Int("length", 80, "Length of each line for stdout")
	stderrLines := flag.Int("stderr-lines", 10, "Number of lines to print to stderr")
	stderrLineLength := flag.Int("stderr-length", 80, "Length of each line for stderr")
	exitCode := flag.Int("exit-code", 0, "Exit code for the program")
	delayMs := flag.Int("delay-ms", 0, "Delay in milliseconds between printing lines")

	attemptCounterFile := flag.String("attempt-counter-file", "", "Path to a file for tracking attempts (for retry tests)")
	failUntilAttempt := flag.Int("fail-until-attempt", 0, "If using -attempt-counter-file, fail until this attempt number is reached")

	flag.Parse()

	if *attemptCounterFile != "" {
		currentExecution := 1
		content, err := os.ReadFile(*attemptCounterFile)
		if err == nil {
			fmt.Sscan(string(content), &currentExecution)
		}

		fmt.Fprintf(os.Stdout, "OUTPUT_GENERATOR_RETRY_MODE: execution %d, target success on execution #%d, pid %d\n", currentExecution, *failUntilAttempt, os.Getpid())

		childExitCode := 1
		if currentExecution >= *failUntilAttempt {
			fmt.Fprintf(os.Stdout, "OUTPUT_GENERATOR_RETRY_MODE: Succeeded on execution %d\n", currentExecution)
			childExitCode = 0
		} else {
			fmt.Fprintf(os.Stderr, "OUTPUT_GENERATOR_RETRY_MODE: Failed on execution %d, will retry\n", currentExecution)
		}

		if err := os.WriteFile(*attemptCounterFile, []byte(fmt.Sprintf("%d", currentExecution+1)), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "OUTPUT_GENERATOR_RETRY_MODE: Error writing attempt counter file %s: %v\n", *attemptCounterFile, err)
			os.Exit(99) // Special exit code for counter file write error
		}
		os.Exit(childExitCode)
		return // Should be unreachable
	}

	// Original output generation logic if not in retry mode
	for i := 0; i < *lines; i++ {
		fmt.Println(strings.Repeat("a", *lineLength))
		if *delayMs > 0 {
			time.Sleep(time.Duration(*delayMs) * time.Millisecond)
		}
	}

	for i := 0; i < *stderrLines; i++ {
		fmt.Fprintln(os.Stderr, strings.Repeat("e", *stderrLineLength))
		if *delayMs > 0 {
			time.Sleep(time.Duration(*delayMs) * time.Millisecond)
		}
	}
	os.Exit(*exitCode)
}

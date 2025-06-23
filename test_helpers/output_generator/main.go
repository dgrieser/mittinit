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

	flag.Parse()

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

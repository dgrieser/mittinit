package main

import (
	"log"
	"mittinit/cmd"
	"net/http"
	_ "net/http/pprof" // Import for side effects: registers pprof handlers
)

func main() {
	// Start pprof server
	go func() {
		log.Println("Starting pprof server on :6060")
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			log.Fatalf("Failed to start pprof server: %v", err)
		}
	}()

	cmd.Execute()
}

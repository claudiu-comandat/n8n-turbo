package main

import (
	"log"
	"net/http"
	"os"
	"strings"
)

func startPprof() {
	if strings.ToLower(strings.TrimSpace(os.Getenv("PPROF_ENABLED"))) != "true" {
		return
	}
	address := strings.TrimSpace(os.Getenv("PPROF_ADDRESS"))
	if address == "" {
		address = "127.0.0.1:6060"
	}
	go func() {
		log.Printf("pprof listening on %s", address)
		if err := http.ListenAndServe(address, nil); err != nil {
			log.Printf("pprof stopped: %v", err)
		}
	}()
}

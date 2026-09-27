package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	target := flag.String("url", "http://127.0.0.1:9100/readyz", "readiness endpoint")
	flag.Parse()

	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(*target)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, resp.Status)
		os.Exit(1)
	}
}

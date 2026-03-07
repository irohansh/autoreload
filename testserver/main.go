package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello! Server time: %s\n", time.Now().Format(time.RFC3339))
	})

	log.Println("Test server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
)

const (
	defaultPort = "8181"
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func main() {
	r := mux.NewRouter()
	r.HandleFunc("/vtep", createVtepHandler).Methods("PUT")

	// Startup the server
	port := getEnv("MESHNETD_PORT", defaultPort)
	url := fmt.Sprintf("0.0.0.0:%s", port)
	log.Fatal(http.ListenAndServe(url, r))

}

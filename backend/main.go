package main

import (
	"encoding/json"
	"log"
	"net/http"
)

type MessageResponse struct {
	Message string `json:"message"`
}

func main() {
	mux := http.NewServeMux()

	// API endpoint
	mux.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(MessageResponse{
			Message: "BACKEND RADI!",
		})
	})

	log.Println("Server running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

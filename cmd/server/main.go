package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/CyanAutomation/merm8/internal/api"
)

func main() {
	scriptPath := os.Getenv("PARSER_SCRIPT")
	if scriptPath == "" {
		scriptPath = "/app/parser-node/parse.mjs"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	handler := api.NewHandlerWithScript(scriptPath)
	handler.RegisterRoutes(mux)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("mermaid-lint listening on %s (parser: %s)", addr, scriptPath)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

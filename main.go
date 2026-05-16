package main

import (
	"log"
	"net/http"
	"os"

	"github.com/AntipasBen23/project-backend/api"
	"github.com/AntipasBen23/project-backend/bot"
	"github.com/AntipasBen23/project-backend/exchange"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	client := exchange.NewClient()
	engine := bot.NewEngine(client)
	engine.SetStrategy("RSI_MA")

	server := api.NewServer(engine, client)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	log.Printf("AIEdge Swing backend starting on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

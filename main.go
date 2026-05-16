package main

import (
	"log"
	"net/http"

	"github.com/AntipasBen23/project-backend/api"
	"github.com/AntipasBen23/project-backend/bot"
	"github.com/AntipasBen23/project-backend/exchange"
)

func main() {
	client := exchange.NewClient()
	engine := bot.NewEngine(client)
	engine.SetStrategy("RSI_MA")

	server := api.NewServer(engine, client)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	log.Println("TradeBot backend starting on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	loadEnvFile(".env")
	db, err := openDatabase()
	if err != nil {
		log.Fatal("database startup failed: ", err)
	}
	defer db.Close()

	if err := importOldExpenses(db, "expenses.txt"); err != nil {
		log.Println("legacy import warning:", err)
	}

	port := envOr("PORT", "8080")
	app := newApp(db)
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           app.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Server started at http://localhost:%s", port)
	log.Fatal(server.ListenAndServe())
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

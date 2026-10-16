package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	marketnotify "example.com/market-notify"
)

func main() {
	listen := flag.String("listen", ":8080", "HTTP listen address")
	sellerID := flag.String("seller", "", "seller account to provision")
	buyerID := flag.String("buyer", "", "buyer account to provision")
	flag.Parse()

	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	client := marketnotify.NewClient(key)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *sellerID != "" || *buyerID != "" {
		if err := marketnotify.ProvisionChannels(ctx, client, *sellerID, *buyerID); err != nil {
			log.Fatal(err)
		}
	}

	server := &http.Server{Addr: *listen, Handler: marketnotify.NewServer(client).Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()
	fmt.Printf("market-notify listening on %s\n", *listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

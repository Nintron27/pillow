package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Nintron27/nats-pillow/pillow"
	"github.com/nats-io/nats.go"
)

func main() {
	log.Println("Starting embedded NATS")
	nc, ns, err := pillow.Run(
		pillow.WithInProcessClient(),
	)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Embedded NATS started")

	sigCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Echo back message
	nc.Subscribe("echo", func(msg *nats.Msg) {
		msg.Respond([]byte("echoing:" + string(msg.Data)))
	})

	<-sigCtx.Done()
	log.Println("Shutting down server gracefully")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	if err := ns.Shutdown(ctx); err != nil {
		log.Fatal(err)
	}
	log.Println("Shut down server gracefully")
}

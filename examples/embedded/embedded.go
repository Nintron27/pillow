package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Nintron27/pillow"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func main() {
	log.Println("Starting embedded NATS")

	// All configuration functions passed to Run() start with "With"
	ns, err := pillow.Run(
		pillow.WithNATSServerOptions(&server.Options{
			// Enable JetStream and store in ./nats directory
			JetStream: true,
			StoreDir:  "./nats",
		}),
		pillow.WithLogging(true),
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Embedded NATS started")

	nc, err := ns.NATSClient()
	if err != nil {
		log.Fatal(err)
	}

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

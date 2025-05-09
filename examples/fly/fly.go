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
)

func main() {
	env := os.Getenv("ENV")
	if env == "" {
		env = "dev"
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	ns, err := pillow.Run(
		pillow.WithNATSServerOptions(&server.Options{
			JetStream: true,
			StoreDir:  "./nats",
		}),
		pillow.WithLogging(true),
		pillow.WithSystemUser(true, "admin", "password"),
		pillow.WithPlatformAdapter(ctx, env == "prod", &pillow.FlyioClustering{
			ClusterName: "pillow-hub",
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	cancel()

	sigCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	<-sigCtx.Done()
	ctx, cancel = context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	if err := ns.Shutdown(ctx); err != nil {
		log.Fatal(err)
	}
}

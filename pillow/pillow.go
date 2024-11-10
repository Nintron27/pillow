package pillow

import (
	"context"
	"errors"
	"log"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

type options struct {
	server           *server.Server
	Timeout          time.Duration // Time to wait for embedded NATS to start
	InProcessClient  bool          // The returned client will communicate in process with the NATS server if enabled
	EnableLogging    bool          // Enable NATS Logger
	NATSSeverOptions *server.Options
}

type Option func(*options)

type Server struct {
	NatsServer *server.Server
	opts       *options
}

// Duration to wait for embedded NATS to start. Defaults to 5 seconds if omitted
func WithTimeout(t time.Duration) Option {
	return func(o *options) {
		o.Timeout = t
	}
}

// If enabled the returned client from Run() will communicate in-process and not over the network layer
func WithInProcessClient(b bool) Option {
	return func(o *options) {
		o.InProcessClient = b
	}
}

// Enable NATS internal logging
func WithLogging(b bool) Option {
	return func(o *options) {
		o.EnableLogging = b
	}
}

// Configure JetStream
func WithJetStream(dir string) Option {
	return func(o *options) {
		o.NATSSeverOptions.JetStream = true
		o.NATSSeverOptions.StoreDir = dir
	}
}

type FlyioOptions struct {
	ClusterName string
}

// Configures the server to run in the Flyio environment. Will cluster all machines in the same app and region
func AdapterFlyio(enable bool, flyopts FlyioOptions) Option {
	if !enable {
		return func(o *options) {}
	}

	flyMachineId := os.Getenv("FLY_MACHINE_ID")
	if flyMachineId == "" {
		panic("Flyio environment variable not found, are you running on Flyio?")
	}
	routes, err := getRoutesFlyio(context.TODO())
	if err != nil {
		panic(err)
	}

	return func(o *options) {
		o.NATSSeverOptions.ServerName = "flynode-" + flyMachineId
		o.NATSSeverOptions.Routes = routes
		o.NATSSeverOptions.Cluster = server.ClusterOpts{
			ConnectRetries: 5,
			Name:           flyopts.ClusterName,
			Port:           4248,
		}

		go func() {
			for {
				time.Sleep(time.Second * 10)
				log.Println("Checking fly routes...")

				if o.server == nil {
					log.Println("Server not started yet...")
					continue
				}

				routes, err := getRoutesFlyio(context.TODO())
				if err != nil {
					log.Println("ERR!!!...")
					continue
				}

				// Check if the routes have changed
				oldRoutes := make([]string, 0, len(o.NATSSeverOptions.Routes))
				for _, route := range o.NATSSeverOptions.Routes {
					oldRoutes = append(oldRoutes, route.String())
				}
				newRoutes := make([]string, 0, len(routes))
				for _, route := range routes {
					newRoutes = append(newRoutes, route.String())
				}

				log.Println("TEST 1")
				log.Println(oldRoutes)
				log.Println(newRoutes)
				if unorderedEqual(newRoutes, oldRoutes) {
					log.Println("Routes are the same...")
					continue
				}

				log.Println("Reloading routes...")
				options := o.NATSSeverOptions.Clone()
				options.Routes = routes
				err = o.server.ReloadOptions(options)
				if err != nil {
					log.Println("Err reloading routes")
					log.Println(err)
					continue
				}
				o.NATSSeverOptions.Routes = routes
			}
		}()
	}
}

// Apply your own NATs Server Options. Note, this will override
// any existing options, so it's recommended to place first
// in the list of options in Run()
func WithNATSServerOptions(natsServerOpts *server.Options) Option {
	return func(o *options) {
		o.NATSSeverOptions = natsServerOpts
	}
}

var Client *nats.Conn

// Applies all passed options and started the NATS server
func Run(opts ...Option) (*nats.Conn, *Server, error) {
	// Set default options, then override with their configured options
	options := &options{
		Timeout:         time.Second * 5,
		InProcessClient: true,
		EnableLogging:   false,
		NATSSeverOptions: &server.Options{
			NoSigs: true,
		},
	}
	for _, o := range opts {
		o(options)
	}

	ns, err := server.NewServer(options.NATSSeverOptions)
	if err != nil {
		return nil, nil, err
	}

	if options.EnableLogging {
		ns.ConfigureLogger()
	}

	ns.Start()

	if !ns.ReadyForConnections(options.Timeout) {
		return nil, nil, errors.New("NATS startup timed out")
	}

	options.server = ns

	clientsOpts := []nats.Option{}
	if options.InProcessClient {
		clientsOpts = append(clientsOpts, nats.InProcessServer(ns))
	}

	nc, err := nats.Connect(ns.ClientURL(), clientsOpts...)
	if err != nil {
		return nil, nil, err
	}

	Client = nc

	return nc, &Server{
		NatsServer: ns,
		opts:       options,
	}, nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	done := make(chan any)
	go func() {
		if s.opts.NATSSeverOptions.NoSigs {
			s.NatsServer.Shutdown()
		}
		s.NatsServer.WaitForShutdown()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Platform specific environment variable not found
var ErrEnvVarNotFound = errors.New("Platform ENV not found")

func getRoutesFlyio(ctx context.Context) ([]*url.URL, error) {
	// Get fly ENV var
	flyAppName := os.Getenv("FLY_APP_NAME")
	if flyAppName == "" {
		return nil, ErrEnvVarNotFound
	}
	flyRegion := os.Getenv("FLY_REGION")
	if flyRegion == "" {
		return nil, ErrEnvVarNotFound
	}

	flyPrivateIp := os.Getenv("FLY_PRIVATE_IP")
	if flyPrivateIp == "" {
		return nil, ErrEnvVarNotFound
	}
	privateIp := net.ParseIP(flyPrivateIp)

	// lookup machines in same region
	ips, err := net.LookupIP(flyRegion + "." + flyAppName + ".internal")
	if err != nil {
		return nil, err
	}

	urls := make([]*url.URL, 0, len(ips))
	for _, ip := range ips {
		if ip.Equal(privateIp) {
			continue
		}
		clusUrl, err := url.Parse("nats://[" + ip.String() + "]:4248")
		if err != nil {
			panic("Unable to parse NATS cluster url")
		}

		urls = append(urls, clusUrl)
	}

	return urls, nil
}

func unorderedEqual[T comparable](first, second []T) bool {
	if len(first) != len(second) {
		return false
	}
	exists := make(map[T]bool)
	for _, value := range first {
		exists[value] = true
	}
	for _, value := range second {
		if !exists[value] {
			return false
		}
	}
	return true
}

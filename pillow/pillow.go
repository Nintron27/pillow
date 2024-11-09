package pillow

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

type options struct {
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

func WithTimeout(t time.Duration) Option {
	return func(o *options) {
		o.Timeout = t
	}
}

func WithInProcessClient() Option {
	return func(o *options) {
		o.InProcessClient = true
	}
}

func WithLogging(b bool) Option {
	return func(o *options) {
		o.EnableLogging = b
	}
}

func WithJetStream(dir string) Option {
	return func(o *options) {
		o.NATSSeverOptions.JetStream = true
		o.NATSSeverOptions.StoreDir = dir
	}
}

type FlyioOptions struct {
	Enable      bool
	JetStream   bool
	StoreDir    string
	ClusterName string
}

func AdapterFlyio(flyopts FlyioOptions) Option {
	if !flyopts.Enable {
		return func(o *options) {}
	}

	flyMachineId := os.Getenv("FLY_MACHINE_ID")
	if flyMachineId == "" {
		panic("Failed to retrieve Flyio environment variable")
	}
	routes, err := getRoutesFlyio(context.TODO())
	if err != nil {
		panic(err)
	}
	return func(o *options) {
		o.NATSSeverOptions.ServerName = "flynode-" + flyMachineId
		o.NATSSeverOptions.JetStream = flyopts.JetStream
		o.NATSSeverOptions.StoreDir = flyopts.StoreDir
		o.NATSSeverOptions.Routes = routes
		o.NATSSeverOptions.Cluster = server.ClusterOpts{
			ConnectRetries: 5,
			Name:           flyopts.ClusterName,
			Port:           4248,
		}
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

	// lookup machines in same region
	ips, err := net.LookupIP(flyRegion + "." + flyAppName + ".internal")
	if err != nil {
		return nil, err
	}

	urls := make([]*url.URL, 0, len(ips))
	for _, ip := range ips {
		clusUrl, err := url.Parse("nats://[" + ip.String() + "]:4248")
		if err != nil {
			panic("Unable to parse NATS cluster url")
		}

		urls = append(urls, clusUrl)
	}

	return urls, nil
}

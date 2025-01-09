package pillow

import (
	"context"
	"errors"
	"net"
	"strings"
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

type Option func(*options) error

type Server struct {
	NatsServer *server.Server
	opts       *options
}

// Duration to wait for embedded NATS to start. Defaults to 5 seconds if omitted
func WithTimeout(t time.Duration) Option {
	return func(o *options) error {
		o.Timeout = t
		return nil
	}
}

// If enabled the returned client from Run() will communicate in-process and not over the network layer
func WithInProcessClient(b bool) Option {
	return func(o *options) error {
		o.InProcessClient = b
		return nil
	}
}

// Enable NATS internal logging
func WithLogging(b bool) Option {
	return func(o *options) error {
		o.EnableLogging = b
		return nil
	}
}

// Configure JetStream
func WithJetStream(dir string) Option {
	return func(o *options) error {
		o.NATSSeverOptions.JetStream = true
		o.NATSSeverOptions.StoreDir = dir
		return nil
	}
}

// Apply your own NATs Server Options. Note, this will override
// any existing options, so it's recommended to place first
// in the list of options in Run().
func WithNATSServerOptions(natsServerOpts *server.Options) Option {
	return func(o *options) error {
		o.NATSSeverOptions = natsServerOpts
		return nil
	}
}

// Global variable of the nats client created from Run().
//
// If you are starting multiple embedded NATS servers, it's recommended to use
// the client returned from Run() as this Client will just be from the last
// invocation of Run().
var Client *nats.Conn

// Applies all passed options and starts the NATS server, returning a client
// connection, a server struct, and error if there was a problem starting the
// server.
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
		err := o(options)
		if err != nil {
			return nil, nil, err
		}
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

// ##############################################################
// Below are random helper functions, don't care to split out yet
// ##############################################################

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

// unorderedEqualFunc reports whether two unordered slices are equal using an equality
// and an identity function function on each pair of elements. If the lengths
// are different unorderedEqualFunc returns false. Otherwise, the elements entered into a
// map and iterated over until one of the elements dosen't match the element with a matching
// identifier, or if a matching identifier element can't be found.
// func unorderedEqualFunc[S1 ~[]E1, S2 ~[]E2, E1, E2 any, T comparable](
// 	s1 S1,
// 	s2 S2,
// 	id func(any) T,
// 	eq func(E1, E2) bool,
// ) bool {
// 	// slices.EqualFunc()
// 	if len(s1) != len(s2) {
// 		return false
// 	}
// 	idMap := make(map[T]E1)
// 	for _, val1 := range s1 {
// 		idMap[id(val1)] = val1
// 	}
// 	for _, val2 := range s2 {
// 		val1, ok := idMap[id(val2)]
// 		if !ok {
// 			return false
// 		}
// 		if !eq(val1, val2) {
// 			return false
// 		}
// 	}
// 	return true
// }

// All IPs must be of the same type, ie IPv4 or IPv6
func intersectIPs(a, b []net.IP) []net.IP {
	set := make([]net.IP, 0)
	hash := make(map[string]struct{})

	for _, v := range a {
		hash[v.String()] = struct{}{}
	}

	for _, v := range b {
		if _, ok := hash[v.String()]; ok {
			set = append(set, v)
		}
	}

	return set
}

// FindAndRemoveSubstring searches for the first string in the array that contains the substring.
// If found, it removes the substring from that string and returns the modified string and true.
// If not found, it returns an empty string and false.
func findAndRemoveSubstring(arr []string, sub string) (string, bool) {
	for _, str := range arr {
		if strings.Contains(str, sub) {
			return strings.Replace(str, sub, "", 1), true
		}
	}
	return "", false
}

// var ErrJetStreamStartedSolo = errors.New("JetStream started as standalone as cluster failed")

// func startJetStream(s *server.Server, o *server.Options) error {
// 	opts := o.Clone()
// 	cfg := server.JetStreamConfig{
// 		MaxMemory:    opts.JetStreamMaxMemory,
// 		MaxStore:     opts.JetStreamMaxStore,
// 		StoreDir:     opts.StoreDir,
// 		SyncInterval: opts.SyncInterval,
// 		SyncAlways:   opts.SyncAlways,
// 		Domain:       opts.JetStreamDomain,
// 		CompressOK:   true,
// 		UniqueTag:    opts.JetStreamUniqueTag,
// 	}
// 	err := s.EnableJetStream(&cfg)
// 	if err != nil {
// 		if s.JetStreamEnabled() && !s.JetStreamIsClustered() {
// 			return ErrJetStreamStartedSolo
// 		}
// 		return s.DisableJetStream()
// 	}
// 	return nil
// }

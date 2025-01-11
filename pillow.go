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

// ErrStartupTimedOut indicates that the embedded nats server exceeded its startup
// timeout duration.
var ErrStartupTimedOut = errors.New("embedded nats server startup timed out")

type options struct {
	server *server.Server

	withSystemUser     bool
	systemUserUsername string
	systemUserPassword string

	// Time to wait for embedded NATS to start
	timeout time.Duration

	// The returned client will communicate in-process with the NATS server if enabled
	inProcessClient bool

	// Enable NATS Logger
	enableLogging bool

	natsSeverOptions *server.Options
}

type Option func(*options) error

type Server struct {
	// Reference to the underlying nats-server server.Server
	NATSServer *server.Server

	opts *options
}

// Duration to wait for embedded NATS to start. Defaults to 5 seconds if omitted
func WithTimeout(t time.Duration) Option {
	return func(o *options) error {
		o.timeout = t
		return nil
	}
}

// Duration to wait for embedded NATS to start. Defaults to 5 seconds if omitted
func WithSystemUser(enable bool, username, password string) Option {
	return func(o *options) error {
		o.withSystemUser = enable
		o.systemUserUsername = username
		o.systemUserPassword = password

		return nil
	}
}

// If enabled the clients returned from *Server.NATSClient() will
// communicate with the embedded server over the network instead of in-process.
func WithoutInProcessClient(enable bool) Option {
	return func(o *options) error {
		o.inProcessClient = !enable
		return nil
	}
}

// Enable NATS internal logging
func WithLogging(enable bool) Option {
	return func(o *options) error {
		o.enableLogging = enable
		return nil
	}
}

// Apply your own NATs Server Options. Note, this will override
// any existing options, so it's recommended to place first
// in the list of options in Run().
func WithNATSServerOptions(natsServerOpts *server.Options) Option {
	return func(o *options) error {
		o.natsSeverOptions = natsServerOpts
		return nil
	}
}

// Applies all passed options and starts the NATS server, returning a server struct
// and error if there was a problem starting the server.
//
// All configuration functions start with "With". Example WithLogging(true)
func Run(opts ...Option) (*Server, error) {
	// Set default options, then override with their configured options
	options := &options{
		timeout:         time.Second * 5,
		inProcessClient: true,
		enableLogging:   false,
		natsSeverOptions: &server.Options{
			NoSigs: true,
		},
	}
	for _, o := range opts {
		err := o(options)
		if err != nil {
			return nil, err
		}
	}

	// Disable this force when this is fixed: https://github.com/nats-io/nats-server/issues/6358
	options.natsSeverOptions.NoSigs = true

	if options.withSystemUser {
		sysAcc := server.NewAccount(server.DEFAULT_SYSTEM_ACCOUNT)

		options.natsSeverOptions.Accounts = []*server.Account{sysAcc}
		options.natsSeverOptions.SystemAccount = server.DEFAULT_SYSTEM_ACCOUNT
		options.natsSeverOptions.Users = []*server.User{{
			Username: options.systemUserUsername,
			Password: options.systemUserPassword,
			Account:  sysAcc,
		}}
	}

	ns, err := server.NewServer(options.natsSeverOptions)
	if err != nil {
		return nil, err
	}

	if options.enableLogging {
		ns.ConfigureLogger()
	}

	ns.Start()

	if !ns.ReadyForConnections(options.timeout) {
		return nil, ErrStartupTimedOut
	}

	options.server = ns

	return &Server{
		NATSServer: ns,
		opts:       options,
	}, nil
}

// Returns a new nats client.
//
// Client will communicate in-process unless the embedded server was started with
// WithoutInProcessClient(true)
func (s *Server) NATSClient() (*nats.Conn, error) {
	clientsOpts := []nats.Option{}
	if s.opts.inProcessClient {
		clientsOpts = append(clientsOpts, nats.InProcessServer(s.NATSServer))
	}

	return nats.Connect(s.NATSServer.ClientURL(), clientsOpts...)
}

func (s *Server) Shutdown(ctx context.Context) error {
	done := make(chan any)
	if s.NATSServer == nil {
		return nil
	}

	go func() {
		// Re-enable this form of handling when this issue is fixed: https://github.com/nats-io/nats-server/issues/6358
		//
		// if s.opts.NATSSeverOptions.NoSigs {
		// 	s.NATSServer.Shutdown()
		// } else if s.NATSServer.Running() {
		// 	s.NATSServer.Shutdown()
		// }
		s.NATSServer.Shutdown()
		s.NATSServer.WaitForShutdown()
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

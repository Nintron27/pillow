package pillow

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

type FlyioOptions struct {
	// Configure how the Flyio Adapter will organize your NATS topology.
	//
	// ClusteringMode will cluster all intra-region machines, and super cluster regions together.
	// Note, if JS is enabled ALL regions must have >= 3 machines.
	//
	// HubAndSpokeMode requires you to configure a hub region (or regions) and then all machines in
	// other regions will connect to the hub as leaf nodes.
	//
	// ConstellationMode will connect all regions together using leaf node.
	//
	// CustomMode allows you to configure exactly how you want your regions to be organized.
	OperationalMode OperationalMode

	// Configure how the Flyio Adapter handles its Clustering mode.
	ClusteringOptions ClusteringModeOptions

	// Configure how the Flyio Adapter handles its HubAndSpoke mode.
	HubAndSpokeOptions HubAndSpokeModeOptions

	// Configure how the Flyio Adapter handles its Constellation mode.
	ConstellationOptions ConstellationModeOptions
}

type ClusteringModeOptions struct {
	// The name to be used for clustering. Appended will be `-<REGION>` where REGION is the
	// Flyio region this cluster is in.
	ClusterName string
}

type HubAndSpokeModeOptions struct {
	// The regions that should be clustered and superclustered together.
	//
	// If only one region is passed then superclustering will be disabled.
	HubRegions []string

	// The name to be used for the hub's cluster(s).
	//
	// If there is multiple entries in HubRegions then `-<REGION>` (where REGION is the
	// Flyio region) will be appended.
	ClusterName string
}

type ConstellationModeOptions struct {
	// If set to true then clustering within regions will be disabled.
	//
	// Note, regions passed in ClusteredRegions will still have clustering enabled.
	DisableClustering bool

	// Configure which regions should be clustered. If array isn't empty then ONLY the
	// regions passed will have clustering enabled.
	ClusteredRegions []string
}

type manualModeOptions struct {
}

type OperationalMode int

const (
	ClusteringMode OperationalMode = iota
	HubAndSpokeMode
	ConstellationMode
	CustomMode
)

const (
	ClusterPort int = 4232
	GatewayPort int = 4242
)

// Configures the server to run in the Flyio environment. Will cluster all machines in the same app and region
// and supercluster different regions together. This Option should be used last, as it overrides specific
// options like JetStream, to achieve the opinionated behavior.
func AdapterFlyio(enable bool, opts FlyioOptions) Option {
	if !enable {
		return func(o *options) error { return nil }
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	env, err := flyioGetEnvVars()
	if err != nil {
		return func(o *options) error { return err }
	}

	inRegionRoutes, err := getRoutesFlyio(ctx, &env)
	if err != nil {
		// Do nothing, regions will be empty until there is a resolution to
		// this post: https://community.fly.io/t/machine-cannot-read-its-own-internal-dns-entry-on-startup/23278
		//
		// return func(o *options) error { return err }
	}
	// Till issue above is fixed forcefully include self so that when JS is enabled it doesn't crash on
	// startup from empty routes
	if len(inRegionRoutes) == 0 {
		selfURL, err := url.Parse("nats://[" + env.privateIP + "]:" + strconv.Itoa(ClusterPort))
		if err != nil {
			return func(o *options) error { return errors.New("Unable to parse NATS gateway url") }
		}
		inRegionRoutes = append(inRegionRoutes, selfURL)
	}

	clusterName := opts.ClusterName
	var gatewayOpts server.GatewayOpts

	if !opts.DisableSuperClustering {
		clusterName += "-" + env.region
		regions, err := getRegionsFlyio(ctx, &env)
		if err != nil {
			// Do nothing, regions will be empty until there is a resolution to
			// this post: https://community.fly.io/t/machine-cannot-read-its-own-internal-dns-entry-on-startup/23278
			//
			// return func(o *options) error { return err }
		}

		gateways := make([]*server.RemoteGatewayOpts, 0)
		for _, region := range regions {
			urls := make([]*url.URL, 0, len(region.machines))
			for _, machine := range region.machines {
				gatewayUrl, err := url.Parse("nats://[" + machine.ip.String() + "]:" + strconv.Itoa(GatewayPort))
				if err != nil {
					return func(o *options) error {
						return errors.New("Unable to parse NATS gateway url")
					}
				}
				urls = append(urls, gatewayUrl)
			}

			gateways = append(gateways, &server.RemoteGatewayOpts{
				Name: opts.ClusterName + "-" + region.name,
				URLs: urls,
			})
		}

		gatewayOpts = server.GatewayOpts{
			Name:           clusterName,
			Host:           env.privateIP,
			Port:           GatewayPort,
			ConnectRetries: 5,
			Gateways:       gateways,
		}
	}

	return func(o *options) error {
		if o.NATSSeverOptions.JetStream {
			if !slices.Contains(opts.JSRegions, env.region) {
				o.NATSSeverOptions.JetStream = false
			}
		}

		o.NATSSeverOptions.ServerName = "fly-" + env.machineID
		o.NATSSeverOptions.Routes = inRegionRoutes
		o.NATSSeverOptions.Gateway = gatewayOpts
		o.NATSSeverOptions.Cluster = server.ClusterOpts{
			ConnectRetries: 5,
			Name:           clusterName,
			Port:           ClusterPort,
		}

		// if !opts.DisableRouteRefetching {
		// 	go func() {
		// 		for {
		// 			time.Sleep(time.Second * 30)

		// 			// Wait till server is started
		// 			if o.server == nil {
		// 				continue
		// 			}

		// 			// If server isn't running stop reloading routes
		// 			if !o.server.Running() {
		// 				return
		// 			}

		// 			var wg sync.WaitGroup
		// 			ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		// 			wg.Add(1)
		// 			go func() {
		// 				defer cancel()
		// 				defer wg.Done()

		// 				err := flyioReloadRoutes(ctx, o, &opts, &env)
		// 				if err != nil {
		// 					// Not really sure what to do with this error
		// 					return
		// 				}
		// 			}()
		// 			wg.Wait()
		// 		}
		// 	}()
		// }

		return nil
	}
}

// flyioReloadRoutes will fetch the current cluster and gateway routes, comparing them
// to the server's current configuration and reloading if they have changed.
func flyioReloadRoutes(ctx context.Context, opts *options, _ *FlyioOptions, env *flyioEnv) error {
	inRegionroutes, err := getRoutesFlyio(ctx, env)
	if err != nil {
		return err
	}

	// Check if the cluster routes have changed
	clusterChanged := false
	oldClusterRoutes := make([]string, 0, len(opts.NATSSeverOptions.Routes))
	for _, route := range opts.NATSSeverOptions.Routes {
		oldClusterRoutes = append(oldClusterRoutes, route.String())
	}
	newClusterRoutes := make([]string, 0, len(inRegionroutes))
	for _, route := range inRegionroutes {
		newClusterRoutes = append(newClusterRoutes, route.String())
	}
	if !unorderedEqual(newClusterRoutes, oldClusterRoutes) {
		clusterChanged = true
	}

	// Gateway reloading disabled till resolution on: https://github.com/nats-io/nats-server/issues/6321
	//
	// Check if the gateways have changed
	// gatewaysChanged := false
	// regions, err := getRegionsFlyio(ctx, env)
	// if err != nil {
	// 	return err
	// }
	// gateways := make([]*server.RemoteGatewayOpts, 0)
	// for _, region := range regions {
	// 	urls := make([]*url.URL, 0, len(region.machines))
	// 	for _, machine := range region.machines {
	// 		gatewayUrl, err := url.Parse("nats://[" + machine.ip.String() + "]:" + strconv.Itoa(GatewayPort))
	// 		if err != nil {
	// 			panic("Unable to parse NATS gateway url")
	// 		}
	// 		urls = append(urls, gatewayUrl)
	// 	}

	// 	gateways = append(gateways, &server.RemoteGatewayOpts{
	// 		Name: flyOpts.ClusterName + "-" + region.name,
	// 		URLs: urls,
	// 	})
	// }
	// gatewaysChanged = !unorderedEqualFunc(
	// 	gateways,
	// 	opts.NATSSeverOptions.Gateway.Gateways,
	// 	// Identifier func
	// 	func(g *server.RemoteGatewayOpts) string {
	// 		return g.Name
	// 	},
	// 	// Equality func
	// 	func(g1, g2 *server.RemoteGatewayOpts) bool {
	// 		return slices.EqualFunc(g1.URLs, g2.URLs, func(url1, url2 *url.URL) bool {
	// 			return url1.String() == url2.String()
	// 		})
	// 	},
	// )

	if !clusterChanged {
		return nil
	}

	// Reload routes
	options := opts.NATSSeverOptions.Clone()
	options.Routes = inRegionroutes
	// See above as to why commented out
	// options.Gateway.Gateways = gateways
	err = opts.server.ReloadOptions(options)
	if err != nil {
		return err
	}
	opts.NATSSeverOptions.Routes = inRegionroutes
	// See above as to why commented out
	// opts.NATSSeverOptions.Gateway.Gateways = gateways

	return nil
}

// A platform specific environment variable could not be found. This should only occur
// if you are trying to use a platform adapter like AdapterFlyio() while not actually
// deploying on Flyio.
//
// If this error does occur when still deploying on the correct platform respective to
// your Adapter, this may be that the platform changed their environment implementation.
// If so, file an issue.
var ErrEnvVarNotFound = errors.New("Platform ENV not found")

// Should only occur when a platform changes something in their implementation. Pillow makes
// assumeptions that platforms tooling will not change, but if it does this error is returned.
var ErrPlatformImplementationChanged = errors.New("Platform implementation changed")

type flyioEnv struct {
	machineID    string // FLY_MACHINE_ID
	appName      string // FLY_APP_NAME
	processGroup string // FLY_PROCESS_GROUP
	region       string // FLY_REGION
	privateIP    string // FLY_PRIVATE_IP
}

func flyioGetEnvVars() (flyioEnv, error) {
	env := flyioEnv{}
	env.machineID = os.Getenv("FLY_MACHINE_ID")
	if env.machineID == "" {
		return flyioEnv{}, ErrEnvVarNotFound
	}
	env.appName = os.Getenv("FLY_APP_NAME")
	if env.appName == "" {
		return flyioEnv{}, ErrEnvVarNotFound
	}
	env.processGroup = os.Getenv("FLY_PROCESS_GROUP")
	if env.processGroup == "" {
		return flyioEnv{}, ErrEnvVarNotFound
	}
	env.region = os.Getenv("FLY_REGION")
	if env.region == "" {
		return flyioEnv{}, ErrEnvVarNotFound
	}
	env.privateIP = os.Getenv("FLY_PRIVATE_IP")
	if env.privateIP == "" {
		return flyioEnv{}, ErrEnvVarNotFound
	}

	return env, nil
}

type regionInfo struct {
	name     string        // Name of the region
	machines []machineInfo // Info on the machines in the region
}

type machineInfo struct {
	id string
	ip net.IP
}

// getRegionsFlyio will return an array of regions this process group exists in, along with
// machine info for all the machines in the same process group in each region.
func getRegionsFlyio(ctx context.Context, env *flyioEnv) ([]regionInfo, error) {
	var r net.Resolver

	// lookup machines in same process group
	processIPs, err := r.LookupIP(ctx, "ip6", env.processGroup+".process."+env.appName+".internal")
	if err != nil {
		return nil, err
	}

	// TODO: Clean this up if there is a conclusion to https://community.fly.io/t/how-to-query-the-regions-a-process-group-exists-in/23189
	//
	// lookup ALL machines in the org, intersect with processIPs, return the unique regions leftover
	instancesDNSResult, err := r.LookupTXT(ctx, "_instances.internal")
	if err != nil {
		return nil, err
	}

	if len(instancesDNSResult) != 1 {
		return nil, ErrPlatformImplementationChanged
	}

	regions := make(map[string]regionInfo, 0)
	instancesCSV := strings.Split(instancesDNSResult[0], ";")
	for _, v := range instancesCSV {
		sections := strings.Split(v, ",")
		if len(sections) != 4 {
			return nil, ErrPlatformImplementationChanged
		}

		instanceID, found := findAndRemoveSubstring(sections, "instance=")
		if !found {
			return nil, ErrPlatformImplementationChanged
		}
		machineIP, found := findAndRemoveSubstring(sections, "ip=")
		if !found {
			return nil, ErrPlatformImplementationChanged
		}
		region, found := findAndRemoveSubstring(sections, "region=")
		if !found {
			return nil, ErrPlatformImplementationChanged
		}

		// If ip isn't in process, filter out
		if !slices.ContainsFunc(processIPs, func(ip net.IP) bool {
			return ip.String() == machineIP
		}) {
			continue
		}

		// add to regions, or update if already exists
		if val, exists := regions[region]; exists {
			val.machines = append(val.machines, machineInfo{
				id: instanceID,
				ip: net.ParseIP(machineIP),
			})
			regions[region] = val
			continue
		}
		regions[region] = regionInfo{
			name: region,
			machines: []machineInfo{{
				id: instanceID,
				ip: net.ParseIP(machineIP),
			}},
		}
	}

	regionsArr := make([]regionInfo, 0, len(regions))
	for _, val := range regions {
		regionsArr = append(regionsArr, val)
	}

	return regionsArr, nil
}

// getRoutesFlyio will return a []*url.URL of the ips of machines in the same region
// in the same process group.
func getRoutesFlyio(ctx context.Context, env *flyioEnv) ([]*url.URL, error) {
	privateIp := net.ParseIP(env.privateIP)

	var r net.Resolver
	// lookup machines in same region
	regionIPs, err := r.LookupIP(ctx, "ip6", env.region+"."+env.appName+".internal")
	if err != nil {
		return nil, err
	}
	// lookup machines in same process group
	processIPs, err := r.LookupIP(ctx, "ip6", env.processGroup+".process."+env.appName+".internal")
	if err != nil {
		return nil, err
	}

	ips := intersectIPs(regionIPs, processIPs)

	urls := make([]*url.URL, 0, len(ips))
	for _, ip := range ips {
		if ip.Equal(privateIp) {
			continue
		}
		clusUrl, err := url.Parse("nats://[" + ip.String() + "]:" + strconv.Itoa(ClusterPort))
		if err != nil {
			return nil, errors.New("Unable to parse NATS gateway url")
		}

		urls = append(urls, clusUrl)
	}

	return urls, nil
}

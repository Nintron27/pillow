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

	"github.com/nats-io/nats-server/v2/server"
)

const (
	ClusterPort  int = 4244
	GatewayPort  int = 7244
	LeafNodePort int = 7422
)

type PlatformConfigurator interface {
	Configure(context.Context) Option
}

// FlyioClustering will cluster all intra-region machines, and super cluster regions together.
// Note, if JetStream is enabled ALL regions must have >= 3 machines.
type FlyioClustering struct {
	// The name to be used for clustering. Appended will be `-<REGION>` where REGION is the
	// Flyio region this cluster is in.
	ClusterName string
}

// FlyioHubAndSpoke will cluster all machines in your primary region, and then all other
// regions will have their machines individually connect to your primary region cluster
// as leaf nodes.
//
// Additionally, the primary region cluster will have the JS domain of `hub` and the leaf nodes
// will have the structure of `leaf-<REGION>-<MACHINE_ID>`
type FlyioHubAndSpoke struct {
	// The name to be used for the primary region cluster.
	ClusterName string
}

func (c *FlyioClustering) Configure(ctx context.Context) Option {
	env, err := flyioGetEnvVars()
	if err != nil {
		return func(o *options) error { return err }
	}

	inRegionRoutes, err := flyioGetRegionURLs(ctx, &env, env.region, "nats", ClusterPort)
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

	clusterName := c.ClusterName + "-" + env.region
	var gatewayOpts server.GatewayOpts

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
			Name: c.ClusterName + "-" + region.name,
			URLs: urls,
		})
	}

	gatewayOpts = server.GatewayOpts{
		Name:           clusterName,
		Port:           GatewayPort,
		ConnectRetries: 5,
		Gateways:       gateways,
	}

	return func(o *options) error {
		o.natsSeverOptions.ServerName = "fly-" + env.machineID
		o.natsSeverOptions.Routes = inRegionRoutes
		o.natsSeverOptions.Gateway = gatewayOpts
		o.natsSeverOptions.Cluster = server.ClusterOpts{
			ConnectRetries: 5,
			Name:           clusterName,
			Port:           ClusterPort,
		}
		return nil
	}
}

func (c *FlyioHubAndSpoke) Configure(ctx context.Context) Option {
	env, err := flyioGetEnvVars()
	if err != nil {
		return func(o *options) error { return err }
	}

	isPrimaryRegion := env.primaryRegion == env.region

	urlProtocol := "nats"
	urlPort := ClusterPort
	if !isPrimaryRegion {
		urlProtocol = "nats-leaf"
		urlPort = LeafNodePort
	}

	primaryRegionURLs, err := flyioGetRegionURLs(ctx, &env, env.primaryRegion, urlProtocol, urlPort)
	if err != nil {
		// Do nothing, regions will be empty until there is a resolution to
		// this post: https://community.fly.io/t/machine-cannot-read-its-own-internal-dns-entry-on-startup/23278
		//
		// return func(o *options) error { return err }
	}
	// Till issue above is fixed forcefully include self so that when JS is enabled it doesn't crash on
	// startup from empty routes, unless it's not primary region, in that case return error as there
	// isn't much we can do to recover from this.
	if len(primaryRegionURLs) == 0 {
		if isPrimaryRegion {
			selfURL, err := url.Parse("nats://[" + env.privateIP + "]:" + strconv.Itoa(ClusterPort))
			if err != nil {
				return func(o *options) error { return errors.New("Unable to parse NATS gateway url") }
			}
			primaryRegionURLs = append(primaryRegionURLs, selfURL)
		} else {
			return func(o *options) error { return errors.New("Unable to get primary region routes") }
		}
	}

	remotes := make([]*server.RemoteLeafOpts, 0)
	if !isPrimaryRegion {
		remotes = append(remotes, &server.RemoteLeafOpts{
			URLs: primaryRegionURLs,
		})
	}

	return func(o *options) error {
		if isPrimaryRegion {
			o.natsSeverOptions.JetStreamDomain = "hub"
			o.natsSeverOptions.LeafNode = server.LeafNodeOpts{
				Port: LeafNodePort,
			}
			o.natsSeverOptions.Routes = primaryRegionURLs
			o.natsSeverOptions.Cluster = server.ClusterOpts{
				ConnectRetries: 5,
				Name:           c.ClusterName,
				Port:           ClusterPort,
			}
		} else {
			o.natsSeverOptions.JetStreamDomain = "leaf-" + env.region + "-" + env.machineID
			o.natsSeverOptions.LeafNode = server.LeafNodeOpts{
				Remotes: remotes,
			}
		}

		o.natsSeverOptions.ServerName = "fly-" + env.machineID
		return nil
	}
}

// The supplied platformCfgr will configure your embedded nats server for a specific network
// topology when deployed on the corrisponding cloud platform.
//
// For local development purposes it's recommended to disable this by checking for an
// environment variable, for example:
//
//	pillow.WithPlatformAdapter(context.TODO(), os.Getenv("ENV") == "prod", &pillow.FlyioClustering{
//	  ClusterName: "pillow-cluster",
//	})
func WithPlatformAdapter(ctx context.Context, enable bool, platformCfgr PlatformConfigurator) Option {
	return platformCfgr.Configure(ctx)
}

// flyioReloadRoutes will fetch the current cluster and gateway routes, comparing them
// to the server's current configuration and reloading if they have changed.
func flyioReloadRoutes(ctx context.Context, opts *options, env *flyioEnv) error {
	inRegionroutes, err := flyioGetRegionURLs(ctx, env, env.region, "nats", ClusterPort)
	if err != nil {
		return err
	}

	// Check if the cluster routes have changed
	clusterChanged := false
	oldClusterRoutes := make([]string, 0, len(opts.natsSeverOptions.Routes))
	for _, route := range opts.natsSeverOptions.Routes {
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
	options := opts.natsSeverOptions.Clone()
	options.Routes = inRegionroutes
	// See above as to why commented out
	// options.Gateway.Gateways = gateways
	err = opts.server.ReloadOptions(options)
	if err != nil {
		return err
	}
	opts.natsSeverOptions.Routes = inRegionroutes
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
	machineID     string // FLY_MACHINE_ID
	appName       string // FLY_APP_NAME
	processGroup  string // FLY_PROCESS_GROUP
	region        string // FLY_REGION
	privateIP     string // FLY_PRIVATE_IP
	primaryRegion string // PRIMARY_REGION
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
	env.primaryRegion = os.Getenv("PRIMARY_REGION")
	if env.primaryRegion == "" {
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

// flyioGetRegionURLs will return a []*url.URL of the ips of machines in the same region
// in the same process group.
func flyioGetRegionURLs(ctx context.Context, env *flyioEnv, region string, protocol string, port int) ([]*url.URL, error) {
	privateIp := net.ParseIP(env.privateIP)

	var r net.Resolver
	// lookup machines in same region
	regionIPs, err := r.LookupIP(ctx, "ip6", region+"."+env.appName+".internal")
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
		// TODO: Potentially remove? it's really fine if IP is in result
		if ip.Equal(privateIp) {
			continue
		}

		clusUrl, err := url.Parse(protocol + "://[" + ip.String() + "]:" + strconv.Itoa(port))
		if err != nil {
			return nil, errors.New("Unable to parse NATS gateway url")
		}

		urls = append(urls, clusUrl)
	}

	return urls, nil
}

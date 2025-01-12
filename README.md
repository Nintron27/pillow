![image](https://github.com/user-attachments/assets/0fce47c6-ac77-4bf9-9899-e551eab874c3)
# Pillow
Remove the boilerplate in your embedded [NATS](https://nats.io/) projects, and deploy to cloud platforms with added functionality!

> [!WARNING]
> The API is unstable and subject to change until v1.0.0 is reached.

## Getting Started
For simply embedding in a Go program:
```shell
go get github.com/Nintron27/pillow
```
then follow the [embedded example](./examples/embedded/embedded.go).

For more examples:
- Reference the [examples folder](./examples) for examples of using pillow.
- This project was started and is being maintained to be used in the [Stelo Finance](https://github.com/stelofinance/stelofinance) project, so check that out for a real project example. 

## Features
- Removes boilerplate when using NATS embedded in Go, and implements sane defaults.
- Platform Adapters that automatically configure clustering, superclustering, and leaf nodes.
- *Your needed feature here*? Leave a feature request issue!

## Platform Adapters Overview
The goal of Platform Adapters is to automatically configure your embedded nats server for certain network topologies when deployed on cloud platforms.

For example, the `FlyioClustering` Platform Adapter, enabled as such:
```go
ns, err := pillow.Run(
  pillow.WithPlatformAdapter(context.TODO(), os.Getenv("ENV") == "prod", &pillow.FlyioClustering{
    ClusterName: "pillow-cluster", // Required, supply whatever value floats your boat
  }),
)
```
will use Flyio's [Machine Runtime Environment](https://fly.io/docs/machines/runtime-environment/) and [.internal DNS](https://fly.io/docs/networking/private-networking/#fly-io-internal-dns) to configure your embedded server to:
1. Cluster with other machines in the same [process group](https://fly.io/docs/launch/processes/) in the same [region](https://fly.io/docs/reference/regions/#discovering-your-apps-region).
2. Form a supercluster with any other regions your process group may be deployed to.
3. Uniquely name your servers and append `-<REGION>` to your clusters' names.

### Flyio
Currently there are only Platform Adapters for [Flyio](https://fly.io/), and the two existing Adapters are:
- `FlyioClustering`: Auto clustering in region, supercluster regions together. ([diagram](https://github.com/Nintron27/pillow/tree/main/examples/fly#flyioclustering))
- `FlyioHubAndSpoke`: Auto cluster primary region, and leaf node other regions' machines to your hub. ([diagram](https://github.com/Nintron27/pillow/tree/main/examples/fly#flyiohubandspoke))

### Quirks w/ Platform Adapters
- You should supply `pillow.WithPlatformAdapter` last in your `pillow.Run()` call as it will override certain nats server options for platform specific reasons, such as overridding the server name so they are unique on every machine.
- `FlyioClustering`: Removing a region will cause any remaining machines will infinitely try to reconnect to the removed region, until they are restarted. This is probably fine, as it's just some network calls, but it is something to be aware of.
- `FlyioClustering`: When JetStream is enabled all your regions must have >= 3 nodes, as JetStream clustering requires this for a quorum.

> [!CAUTION]
> Take great caution when switching adapters (such as HubAndSpoke to Clustering) as the cluster names and JS domains will change.
> Also caution with scaling down JS regions, as removing a node that contains a stream or kv bucket can cause an outage. You should evict them as a peer before removing the machine.

## Forced Opinions
- `NoSigs` is forced to `true` for the nats-server configuration, as it generally doesn't align with embedding NATS in Go, but also causes problems with the Shutdown function due to nats-io/nats-server#6358.

*Credit to [@whaaaley](https://github.com/whaaaley) for project name and icon, and [@delaneyj](https://github.com/delaneyj) for the inspiration.*

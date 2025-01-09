![Pillow icon](https://github.com/user-attachments/assets/7ed49aab-a998-4bef-80b4-230b6ad87690)

# Pillow
A soft little wrapper for [NATS](https://nats.io/) ^-^

Remove the boilerplate in your embedded NATS projects, and deploy to cloud platforms with added functionality!

> [!WARNING]
> The Pillow API is unstable until v1.0.0 and as such may be subject to change in future releases until v1.0.0 is reached.

## Getting Started
For simply embedding in a Go program:

```shell
go get github.com/Nintron27/pillow
```

then follow the [embedded example](./examples/embedded/embedded.go).

## Features
- Remove boilerplate when using NATS embedded in Go
- Flyio Adapters (auto clustering w/ route updates, node naming based on fly machine ID)
  - Clustering: Auto clustering in region, supercluster regions together.
  - HubAndSpoke: Auto cluster primary region, and leaf node other regions' machines to your hub.
- <Your needed feature here>? Leave a feature request issue!

## Examples
- Reference the [examples folder](./examples) for examples of using nats-pillow.
- This project was started and is being maintained to be used in the [Stelo Finance](https://github.com/stelofinance/stelofinance) project, so check that out for a real project example. 

## Quirks with Platform Adapters
- FlyioClustering: Removing a region will cause any remaining machines will infinitely try to reconnect to the removed region, until they are restarted. This is probably fine, as it's just some network calls, but it is something to be aware of.
- FlyioClustering: When JetStream is enabled all your regions must have >= 3 nodes, as JetStream clustering requires this for a quorum.
- Flyio (Clustering & HubAndSpoke): Scaling down a JetStream enabled region may cause the JS quorum to fail, or the meta leader to not be established. This renders JS unuseable, and will hopefully be fixed or addressed in a future release.

> [!CAUTION]
> Take great caution when switching adapters (such as HubAndSpoke to Clustering) as the cluster names and JS domains will change.
> Also caution with scaling down JS regions, as there seems to be a bug with quorums failing.

*Credit to [@whaaaley](https://github.com/whaaaley) for project name and icon*

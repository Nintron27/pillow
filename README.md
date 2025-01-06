![Nats Pillow icon](https://github.com/user-attachments/assets/7fd40216-2a5d-4ca8-b53e-04710038e718)


# NATS Pillow
A soft little wrapper for NATS, to remove the boilerplate for certain use cases, mainly running NATS embedded in Go.

## Features
- Flyio Adapter (auto clustering w/ route updates, node naming based on fly machine ID)
- Boilerplate for using NATS embedded in go

## Examples
Reference the [examples folder](./examples) for examples of using nats-pillow. Additionally, this project was started and is being maintained to be used in the [Stelo Finance](https://github.com/stelofinance/stelofinance) project, so check that out for a real project example. 

## Quirks w/ Flyio Adapter
- Removing a region will cause any remaining machines will infinitely try to reconnect to the removed region, until they are restarted. This is probably fine, as it's just some network calls, but it is something to be aware of.
- When JetStream is enabled you need to explicitly pass the regions you want it to be enabled on in the Adapter options. This behavior will hopefully be improved in the future.
- Scaling down a JetStream enabled region may cause the JS quorum to fail, or the meta leader to not be established. This renders JS unuseable, and will hopefully be fixed or addressed in a future release.

*Credit to [@whaaaley](https://github.com/whaaaley) for project name and icon*

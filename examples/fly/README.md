# Flyio Example
This is an example showcasing the basics of what's needed to use pillow for deploying to Flyio, with automatic clustering and superclustering.
As you increase the machines in the same region, pillow will pass in the routes of the existing machines, dynamically creating a cluster.
As you add more regions pillow will pass the gateway options required to form a super cluster.

JetStream is also enabled, and the file directory is configured in fly.toml.

Note, there are some quirks to using JetStream on Flyio w/ pillow currently due to clustering/superclustering.
- If you scale down and remove a region any remaining existing machines will infinitely try to reconnect to the removed region, until they are restarted. This is probably fine, as it's just some network calls, but it is something to be aware of.
- When JS is enabled you need to explicitly pass the regions you want it to be enabled on in the Adapter options.


## Running Example
This example is actually used locally for testing, so the Dockerfile assumes you are in the root directory of the project, and as such requires a `go.work` file at projcet root.

```
go 1.23.1

use ./

use ./examples/fly
```

then to run this example you should use: `fly deploy --dockerfile "./examples/fly/Dockerfile" -c "./examples/fly/fly.toml"`

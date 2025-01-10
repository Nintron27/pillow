# Flyio Example
This is an example showcasing the basics of what's needed to use pillow for deploying to Flyio, with automatic clustering and superclustering.
As you increase the machines in the same region, pillow will pass in the routes of the existing machines, dynamically creating a cluster.
As you add more regions pillow will pass the gateway options required to form a super cluster.

JetStream is also enabled, and the file directory is configured in fly.toml.

Note, there are some quirks to using JetStream on Flyio w/ pillow currently due to clustering/superclustering.
- If you scale down and remove a region any remaining existing machines will infinitely try to reconnect to the removed region, until they are restarted. This is probably fine, as it's just some network calls, but it is something to be aware of.
- When JS is enabled you need to have >= 3 nodes in each region for JS to reach a quorum.

## Running Example
This example is actually used locally for testing, so the Dockerfile assumes you are in the root directory of the project, and as such requires a `go.work` file at projcet root.

```
go 1.23.1

use ./

use ./examples/fly
```

then to run this example you should use: `fly deploy --dockerfile "./examples/fly/Dockerfile" -c "./examples/fly/fly.toml"`

## Network Topology Diagram
![image](https://github.com/user-attachments/assets/4e0a66b1-da72-4a19-846a-5a0c573a04e0)

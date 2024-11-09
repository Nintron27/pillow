# Flyio Example
This is an example showcasing the basics of what's needed to use pillow for deploying to Flyio, with automatic clustering.
As you increase the machines in the same region, pillow will pass in the routes of the existing machines, dynamically creating a cluster.

JetStream is also enabled, and the file directory is configured in fly.toml

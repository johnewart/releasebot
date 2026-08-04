# Local development helpers for releasebot.
# Requires: https://github.com/casey/just

bin := "releasebot"

# Compile the CLI to ./releasebot in the repo root.
build:
    go build -o {{bin}} .

# Run the GitHub App HTTP server (webhooks, OAuth, git-flow visualization).
serve: build
    ./{{bin}} app serve --addr :8080

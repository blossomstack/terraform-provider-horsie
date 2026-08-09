# Machines on Fly. The app must already exist -- horsie creates machines, not
# apps -- and the image must have horsie-runtime baked in.
resource "horsie_runtime_vendor" "fly" {
  name       = "fly-prod"
  credential = var.fly_api_token

  fly {
    app            = "horsie-runtimes"
    image          = "ghcr.io/blossomstack/horsie-runtime:latest"
    region         = "iad"
    workspace_root = "/workspaces"
    # The full URL, including the connect path: horsie stores what it is sent.
    callback_url   = "wss://horsie.example.com/api/runtime/connect"
    volumes        = true
    cpu_kind       = "shared"
    cpus           = 1
    memory_mb      = 1024
    volume_size_gb = 10
  }
}

# Containers on velos. A velos deployment may run without auth, in which case
# the credential is empty -- it cannot be omitted when creating, only emptied.
resource "horsie_runtime_vendor" "velos" {
  name       = "velos-lab"
  credential = ""

  velos {
    server_url     = "http://velos:8080"
    image          = "ghcr.io/blossomstack/horsie-runtime:latest"
    runtime_bin    = "horsie-runtime"
    workspace_root = "/workspaces"
    # Reachable from velos's container network, not from your browser.
    callback_url = "ws://horsie.internal:3789/api/runtime/connect"
    cpu          = 1
    memory_mb    = 1024
  }
}

resource "horsie_mcp_server" "linear" {
  name = "linear"
  url  = "https://mcp.linear.app/mcp"

  auth {
    kind  = "bearer"
    token = var.linear_token
  }
}

resource "horsie_mcp_server" "docs" {
  name = "docs"
  url  = "https://docs.example.com/mcp"

  # Public server: say so, rather than leaving the block out.
  auth {
    kind = "none"
  }
}

resource "horsie_mcp_server" "github" {
  name = "github"
  url  = "https://api.githubcopilot.com/mcp/"

  # Reuses horsie's existing GitHub App connection; no credential is stored here.
  auth {
    kind = "github_app"
  }
}

resource "horsie_agent" "triage" {
  name        = "triage"
  model       = horsie_model.sonnet.alias
  mcp_servers = [horsie_mcp_server.linear.name, horsie_mcp_server.github.name]
}

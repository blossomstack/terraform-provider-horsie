resource "horsie_model" "sonnet" {
  alias          = "sonnet"
  model_provider = horsie_model_provider.anthropic.name
  model_id       = "claude-sonnet-4-6"

  thinking_efforts = ["none", "low", "high"]
  thinking_effort  = "low"
}

resource "horsie_model_provider" "anthropic" {
  name    = "anthropic"
  kind    = "anthropic"
  api_key = var.anthropic_api_key

  # Genuine Anthropic validates thinking-block signatures on replay; leave this
  # off for Anthropic-compatible endpoints, which do not.
  keep_thinking_signature = true
}

terraform {
  required_providers {
    horsie = {
      source = "blossomstack/horsie"
    }
  }
}

# endpoint and token also read HORSIE_ENDPOINT / HORSIE_TOKEN.
provider "horsie" {
  endpoint = "https://horsie.example.com"
}

resource "horsie_model_provider" "anthropic" {
  name    = "anthropic"
  kind    = "anthropic"
  api_key = var.anthropic_api_key

  # Genuine Anthropic validates thinking-block signatures on replay.
  keep_thinking_signature = true
}

resource "horsie_model" "sonnet" {
  alias          = "sonnet"
  model_provider = horsie_model_provider.anthropic.name
  model_id       = "claude-sonnet-4-6"

  thinking_efforts = ["none", "low", "high"]
  thinking_effort  = "low"
}

variable "anthropic_api_key" {
  type      = string
  sensitive = true
}

resource "horsie_environment" "ci" {
  name        = "ci"
  description = "Checkout of the main repo on the build fleet"
  vendor      = "fleet"

  repos {
    url     = "https://github.com/blossomstack/horsie"
    git_ref = "main"
  }

  env_var {
    name  = "RUST_LOG"
    value = "info"
  }
}

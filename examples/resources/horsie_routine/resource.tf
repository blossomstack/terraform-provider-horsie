resource "horsie_routine" "nightly" {
  name   = "nightly-review"
  agent  = horsie_agent.reviewer.name
  prompt = "Review anything that landed today and summarise what needs attention."

  # Required: horsie has no default for where a run happens.
  environment {
    type   = "runtime"
    vendor = "local"
  }

  schedule {
    type     = "daily"
    timezone = "UTC"
    hour     = 3
    minute   = 30
  }
}

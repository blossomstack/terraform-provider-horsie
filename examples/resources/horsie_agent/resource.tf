resource "horsie_agent" "reviewer" {
  name        = "reviewer"
  description = "Reviews pull requests"
  model       = horsie_model.sonnet.alias

  memory_spaces = [horsie_memory_space.notes.name]
  plugins       = ["code-review"]
}

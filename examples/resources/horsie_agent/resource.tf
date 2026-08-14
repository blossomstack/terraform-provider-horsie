resource "horsie_agent" "reviewer" {
  name        = "reviewer"
  description = "Reviews pull requests"
  model       = horsie_model.sonnet.alias

  memory_spaces = [horsie_memory_space.notes.name]
  plugins       = ["code-review"]

  # Standing instructions ride in every prompt this preset's agent sends.
  instructions = <<-EOT
    Review for correctness first and style last. Cite every finding as
    file:line, and say plainly when a change is fine as it stands.
  EOT
}

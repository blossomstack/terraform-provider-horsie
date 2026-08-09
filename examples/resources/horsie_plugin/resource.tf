resource "horsie_plugin" "toolkit" {
  source_url = "https://github.com/acme/horsie-toolkit"

  # Pin a commit sha, not a branch: horsie clones once, so a branch would
  # install whatever happened to be there on the day of the apply.
  source_ref = "9f3c1abf0d2e4c5a6b7c8d9e0f1a2b3c4d5e6f70"

  # Pre-select it in the new-session picker.
  enabled_default = true
}

# The bundle's name is assigned by the server, so reference it rather than
# guessing it.
output "toolkit_skills" {
  value = [for entry in horsie_plugin.toolkit.catalog : entry.name if entry.kind == "skill"]
}

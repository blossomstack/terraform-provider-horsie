data "horsie_runtime_vendors" "all" {}

# Only a vendor that builds a workspace it owns can back an environment.
output "provisioning_vendors" {
  value = [
    for v in data.horsie_runtime_vendors.all.vendors : v.name
    if v.supports_provisioning
  ]
}

# What new sessions target when they name no vendor. A server has one of these.
resource "horsie_default_runtime_vendor" "this" {
  vendor = horsie_runtime_vendor.fly.name
}

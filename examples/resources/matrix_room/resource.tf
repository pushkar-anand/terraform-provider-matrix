resource "matrix_room" "ops" {
  name  = "Ops"
  topic = "Alerts and incident chatter"

  # Applied at creation only -- Matrix cannot re-apply a preset afterwards.
  preset          = "private_chat"
  alias_localpart = "ops"

  # Listing in the public directory is independent of the join rules above.
  visibility = "private"
}

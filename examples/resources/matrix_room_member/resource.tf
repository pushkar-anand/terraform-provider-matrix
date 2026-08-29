resource "matrix_room" "ops" {
  name   = "Ops"
  preset = "private_chat"
}

resource "matrix_user" "alice" {
  localpart           = "alice"
  display_name        = "Alice"
  password_wo         = var.alice_password
  password_wo_version = "1"
}

# Put the account in the room outright. This needs the Synapse admin API and
# only works for local users: Matrix will not let one account accept an
# invitation on another's behalf.
resource "matrix_room_member" "alice_ops" {
  room_id = matrix_room.ops.id
  user_id = matrix_user.alice.id
}

# Invite instead, and let the person decide. Accepting is not drift -- Terraform
# can withdraw an invitation but cannot un-accept one -- so this stays clean
# once they join.
resource "matrix_room_member" "bob_ops" {
  room_id    = matrix_room.ops.id
  user_id    = "@bob:example.org"
  membership = "invite"
  reason     = "On call from Monday"
}

# Membership is also how a ban is expressed. Destroying this lifts it.
resource "matrix_room_member" "spammer_ops" {
  room_id    = matrix_room.ops.id
  user_id    = "@spammer:elsewhere.example"
  membership = "ban"
  reason     = "Repeated spam"
}

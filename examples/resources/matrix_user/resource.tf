# Passwords are write-only arguments: Terraform hands the value to the provider
# and never writes it to state or to a plan file. An ephemeral variable keeps it
# out of both ends, so the secret exists only for the duration of the apply.
variable "agent_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "matrix_user" "alice" {
  localpart    = "alice"
  display_name = "Alice"

  password_wo = var.agent_password
  # Nothing compares the password itself, because nothing stores it. Bump this
  # to send a new one.
  password_wo_version = "1"
}

resource "matrix_user" "bot" {
  localpart    = "notifier"
  display_name = "Notifier"

  password_wo         = var.agent_password
  password_wo_version = "1"

  # Rotate the password without signing the bot out of its existing session.
  logout_devices_on_password_change = false
}

resource "matrix_user" "retired" {
  localpart = "olduser"

  # Deactivation is reversible on its own, but reactivating without also
  # supplying a password leaves the account unable to log in.
  deactivated = true
}

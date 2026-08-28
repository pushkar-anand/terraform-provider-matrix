## 0.2.0 (Unreleased)

FEATURES:

* **New Resource:** `matrix_user`, managing local accounts through the Synapse
  admin API. Passwords are a write-only argument, so they are sent to the
  homeserver without ever being written to state or to a plan file. Using
  `password_wo` requires Terraform 1.11 or later; the rest of the resource does
  not.

NOTES:

* Destroying a `matrix_user` **deactivates** the account rather than deleting
  it, because Matrix has no delete for users. The user ID stays claimed on the
  homeserver permanently and cannot be registered again.

## 0.1.1

FEATURES:

* `matrix_room`: new `encryption` argument, enabling end-to-end encryption ([#4](https://github.com/pushkar-anand/terraform-provider-matrix/pull/4))

BREAKING CHANGES:

* `matrix_room`: rooms are now encrypted by default. Existing unencrypted rooms
  plan an in-place update to enable it, which **cannot be undone** -- Matrix has
  no way to remove `m.room.encryption`. Set `encryption = false` explicitly on
  any room a bot or bridge must read, and review the plan before applying.

## 0.1.0

FEATURES:

* **New Resource:** `matrix_room` ([#2](https://github.com/pushkar-anand/terraform-provider-matrix/pull/2))

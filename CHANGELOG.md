## 0.1.1 (Unreleased)

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

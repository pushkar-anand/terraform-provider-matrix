## 0.3.0

FEATURES:

* **New Resource:** `matrix_room_member`, managing one user's membership of one
  room. Invites, kicks and bans go through the Client-Server API; `join` uses
  the Synapse admin API, because the Matrix auth rules only accept a `join`
  membership event from the joining user themselves, so an administrator can
  invite an account but never accept on its behalf. Forcing a join is therefore
  limited to local users, which the provider rejects at plan time rather than
  letting the homeserver refuse it at apply. That endpoint appends the member
  event as the target user and puts it through the ordinary auth checks without
  inviting on the way, so in a room that admits people by invitation the
  provider sends the invitation first.

  `membership = "invite"` is a floor rather than an exact value: a user who
  accepts is not drift, since Terraform can withdraw an invitation but has no
  way to un-accept one. The `current_membership` attribute reports what the
  homeserver actually holds.

## 0.2.1

BUG FIXES:

* `matrix_user`: creating an account with `admin = false` — the default, and so
  every ordinary account — failed with `M_UNKNOWN: <user> was never an admin`.
  The homeserver maps `admin: false` onto a revoke, and revoking from an account
  that never held admin is an error, so the flag is no longer sent when creating
  a new account. An account the upsert adopts rather than creates is still
  demoted when the configuration asks for it.

## 0.2.0

FEATURES:

* **New Resource:** `matrix_user`, managing local accounts through the Synapse
  admin API. Passwords are a write-only argument, so they are sent to the
  homeserver without ever being written to state or to a plan file. Using
  `password_wo` requires Terraform 1.11 or later; the rest of the resource does
  not.

* **New Resource:** `matrix_media`, uploading a file to the content repository
  and exporting its `mxc://` URI. Matrix profile pictures are content-repository
  references rather than ordinary URLs, so this is what makes a `matrix_user`
  avatar expressible as code. The source file is hashed during planning, so
  editing it in place is detected even though the path has not changed.

NOTES:

* Destroying a `matrix_user` **deactivates** the account rather than deleting
  it, because Matrix has no delete for users. The user ID stays claimed on the
  homeserver permanently and cannot be registered again.

* `matrix_media` uploads are immutable: the homeserver assigns a random media ID
  and the bytes behind it can never be rewritten, so any change replaces the
  upload. Nothing tracks who refers to a given URI, so destroying media that is
  still referenced leaves a broken reference behind.

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

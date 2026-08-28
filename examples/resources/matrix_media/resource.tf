# Matrix profile pictures are content-repository references, not ordinary URLs,
# so an avatar has to be uploaded before it can be set.
resource "matrix_media" "alice_avatar" {
  source       = "${path.module}/avatars/alice.png"
  filename     = "alice.png"
  content_type = "image/png"
}

resource "matrix_user" "alice" {
  localpart    = "alice"
  display_name = "Alice"

  avatar_url = matrix_media.alice_avatar.mxc_uri
}

# The file's contents are hashed on every plan, so replacing avatars/alice.png
# uploads the new one and repoints the avatar, even though nothing in this
# configuration changed. The old upload is deleted.

# For media generated in the configuration rather than read from disk.
resource "matrix_media" "notice" {
  content_base64 = base64encode("Reprovisioned by Terraform.\n")
  filename       = "notice.txt"
  content_type   = "text/plain; charset=utf-8"
}

# Media is imported by its mxc:// URI.
#
# Only the URI can be recovered this way. Terraform cannot tell which local file
# the bytes came from, so a subsequent plan proposes replacing the upload once
# `source` or `content_base64` is added to the configuration.
terraform import matrix_media.alice_avatar 'mxc://example.org/AbCdEfGhIjKlMnOpQr'

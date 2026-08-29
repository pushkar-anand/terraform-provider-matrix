# Membership is imported as the room ID and the user ID, separated by a comma.
# Not a slash: a user ID localpart may contain one.
terraform import matrix_room_member.alice_ops '!abcdef:example.org,@alice:example.org'

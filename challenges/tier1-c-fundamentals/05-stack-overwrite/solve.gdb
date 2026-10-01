# Oracle for tier1-05-stack-overwrite: save the checksum before the copy, restore it after.
break receive
run
next
set $saved = f.checksum
next
set var f.checksum = $saved
continue

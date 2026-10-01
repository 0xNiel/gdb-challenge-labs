# Oracle for tier1-02-null-deref: give greet() the real Bob record.
break greet
run
set var u = &users[2]
continue

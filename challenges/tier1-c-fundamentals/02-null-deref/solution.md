# Null dereference: walkthrough

**Symptom.** `looking up Bob`, then a crash: `Segmentation fault`.

**Where.** Run it under gdb and ask for the stack:

```
(gdb) run
Program received signal SIGSEGV, Segmentation fault.
greet (u=0x0) at /opt/lab/src/main.c:41
41		printf("hello %s, you are level %d\n", u->name, u->level);
(gdb) bt
#0  greet (u=0x0) at /opt/lab/src/main.c:41
#1  main () at /opt/lab/src/main.c:50
```

`greet` was given `u = 0x0`, a null pointer, and reads `u->name`. `main` got `u` from `find_user(who)`, so `find_user` returned null: it found nobody called `Bob`.

**Why.** Look at the table and the compare:

```
(gdb) print users
$1 = {{id = 101, name = "alice", ...}, {id = 205, name = "carol", ...}, {id = 347, name = "bob", ...}, {id = 412, name = "dave", ...}}
(gdb) break 34 if i == 2
(gdb) run
(gdb) print users[i].name
$2 = "bob"
(gdb) print name
$3 = "Bob"
(gdb) print (int) strcmp(users[i].name, name)
$4 = 32
```

(libc has no debug information here, so gdb needs the `(int)` cast to call `strcmp`.) `strcmp` is case-sensitive: `"bob"` and `"Bob"` differ (by 32, the distance between `b` and `B` in ASCII). The record exists; the lookup cannot see it.

**Fix it in gdb.** Hand `greet` the record it should have received:

```
(gdb) break greet
(gdb) run
(gdb) set var u = &users[2]
(gdb) continue
hello bob, you are level 2
report: LAB{...}
```

Alternatively, `finish` out of `find_user` and `set var u = &users[2]` in `main`, or `return &users[2]` from inside `find_user`.

**The real fix:** compare case-insensitively (`strcasecmp`), or normalise names on input; and check for `NULL` before using the result of a lookup.

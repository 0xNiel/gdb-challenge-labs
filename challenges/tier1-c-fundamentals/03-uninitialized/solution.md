# Uninitialized: walkthrough

**Symptom.** `SPRING10` is not one of the valid codes (`WELCOME5`, `SUMMER-10`, `LOYAL15`), yet the program prints `code SPRING10 accepted: 10% off` and charges 45.00 instead of 49.99.

**Find it.** The decision is `lookup(code)`'s return value, `found`. Stop in `lookup` and watch it:

```
(gdb) break lookup
(gdb) run
(gdb) x/dw &found
0x...:	1
(gdb) watch found
(gdb) continue
Watchpoint 2 deleted because the program has left the block in
which its expression is valid.
main () at /opt/lab/src/main.c:...
```

The watchpoint never triggers: in the loop, `found` is written only when a code matches, and none does. `lookup` returns `found` as it started, and it started as **1**. That 1 was left on the stack by `well_formed`, called just before from the same place in `main`: its `ok = 1` sat in exactly the memory `lookup`'s `found` now uses. (The address `x` shows is on the stack and changes between runs; the value does not.)

**Fix it in gdb.** Give `found` the value it should have started with:

```
(gdb) break lookup
(gdb) run
(gdb) set var found = 0
(gdb) continue
code SPRING10 is not valid
to pay: 49.99
report: LAB{...}
```

**The real fix:** `int found = 0;`. Compile with `-Wall -Wextra` (`-Wmaybe-uninitialized`); compilers and tools such as Valgrind or `-fsanitize=memory` catch this class of bug.

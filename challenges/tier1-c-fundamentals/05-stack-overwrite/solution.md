# The overwritten neighbour: walkthrough

**Symptom.** `received 20 bytes`, then `checksum mismatch: efbeadde, dropped`. The checksum computed on arrival and the one checked a line later differ, though nothing in between should touch it.

**Hypothesis.** `f.checksum` was right and something overwrote it. Catch the write:

```
(gdb) break receive
(gdb) run
(gdb) next
(gdb) print/x f.checksum
$1 = 0xb1cecf58
(gdb) watch f.checksum
(gdb) continue
Watchpoint 2: f.checksum
Old value = 2983120728
New value = 4022250974
... in memcpy ...
(gdb) bt
#0  ... memcpy ...
#1  receive (packet=..., len=20) at /opt/lab/src/main.c:...
#2  main () at /opt/lab/src/main.c:...
```

`memcpy` changed it, called from `receive` with `len=20`.

**Confirm with the layout.**

```
(gdb) ptype /o struct frame
/* offset      |    size */  type = struct frame {
/*      0      |      16 */    char buf[16];
/*     16      |       4 */    unsigned int checksum;
```

`memcpy(f.buf, packet, 20)` writes 20 bytes into a 16-byte buffer. Bytes 16 to 19 of the packet (`de ad be ef`) land in `checksum`, which on a little-endian machine reads `0xefbeadde`.

**Fix it in gdb.** Save the checksum before the copy, restore it after:

```
(gdb) break receive
(gdb) run
(gdb) next
(gdb) set $saved = f.checksum
(gdb) next
(gdb) set var f.checksum = $saved
(gdb) continue
packet ok, checksum b1cecf58
report: LAB{...}
```

Or `tbreak` the `if` line and `jump` to it, skipping the copy (the buffer then stays unfilled, which this program does not use afterwards).

**The real fix:** never copy more than the destination holds: `memcpy(f.buf, packet, len < sizeof f.buf ? len : sizeof f.buf)`, or reject packets longer than the buffer. Compilers' `-D_FORTIFY_SOURCE=2` and `-fsanitize=address` catch this class of overflow.

# Boss: the overwritten neighbour

You have the tools: breakpoints and stepping, the stack, watchpoints, raw memory. This lesson is about using them together on a bug where the variable that goes wrong is not the one the code touches. That is the hardest kind of bug to find by reading, and the one a debugger finds best. There is no new syntax beyond a few commands; most of this is method.

## A method

1. **Pin down the symptom.** What exactly is wrong, and from which line onwards? A wrong value printed at line 40 was wrong before line 40; find the earliest point where it is already wrong.
2. **Form a hypothesis.** "This variable was right, and something changed it." Or "it was never right."
3. **Catch the change.** A watchpoint on the variable stops at the instruction that writes it, wherever that is: your code, a library function, a loop three calls down.
4. **Confirm with the stack.** When the watchpoint fires, `bt` shows who was writing. A write from inside `memcpy` called by your function is a very different story from a write by your own assignment.
5. **Choose the least invasive fix.** Change as little state as you can, then let the program run on. If it behaves, your hypothesis was right.

## Catching a write you did not write

Watchpoints fire on *any* change to the watched memory, including writes by code that never names the variable:

```
(gdb) watch rec.crc
Watchpoint 2: rec.crc
(gdb) continue
Watchpoint 2: rec.crc
Old value = 3735928559
New value = 1162167621
__memcpy_fwd (...) at src/string/memcpy.c:...
(gdb) bt
#0  __memcpy_fwd (...)
#1  load (rec=..., data=..., n=24) at loader.c:22
#2  main () at loader.c:41
```

`load` never assigns `rec.crc`, but its `memcpy` just did. Frame #1 shows the call and its arguments: `n=24`. Now compare that with the destination's size.

## Layout, again

`ptype /o struct record` shows which fields follow which and how big each is. If a 16-byte buffer is followed by a 4-byte field and someone copies 20 bytes into the buffer, the last 4 bytes land in that field. On the stack the same thing happens to whatever the compiler placed next: another local, a saved register, the return address. That is the classic stack overflow. Here it hits a field you can watch, which is kinder.

`info symbol ADDR` names what lives at a code or global address: `info symbol 0x401170` gives `load + 32 in section .text`. `x/32xw $sp` (or `$rsp` on x86-64) shows the top of the stack as 32 words, a quick way to see a buffer and its neighbours side by side.

## Saving a value for later

gdb has convenience variables: any name starting with `$`. They live in gdb, not in the program:

```
(gdb) set $before = rec.crc
(gdb) next
(gdb) print $before
```

Save a value before a line runs, let it run, compare or restore.

## jump

`jump LINE` moves execution to `LINE` and continues from there, *skipping* everything in between. It is the bluntest fix: the skipped code does not run at all. If the skipped line was the bad copy, the buffer never gets filled. Sometimes that is fine for a test, often it is not. Prefer repairing the one value that went wrong, and use `jump` when the bad step has no other effect you need. (`tbreak LINE` before `jump LINE` stops there again instead of running on.)

## Try it

This is the tier's boss: no hints. A program receives a network packet, computes its checksum, stores the packet, checks the checksum again, and drops the packet as corrupted. The packet was fine. Find what changed the checksum, prove it with the stack and the layout, and let the packet through with the checksum it really had.

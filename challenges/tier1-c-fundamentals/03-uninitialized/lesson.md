# State you didn't set: uninitialized memory

`int count;` declares a variable. It does not give it a value. In C, a local variable without an initialiser holds whatever bytes happen to be in its memory when the function starts. That is often zero, often not, and rarely the same across compilers, optimisation levels or machines. Programs that read such a variable work by luck, and luck runs out. This lesson shows where those bytes come from and how to catch the moment a variable gets, or fails to get, its value.

## Stack memory is reused

Local variables live in the function's stack frame. When a function returns, its frame is not wiped: the stack pointer simply moves back, and the next function called from the same place gets the *same* memory for its own frame. Whatever the previous function left there is what the new one's uninitialised locals start with.

```c
void first(void)  { int secret = 1234; }
void second(void) { int x; printf("%d\n", x); }

int main(void) { first(); second(); }   /* very likely prints 1234 */
```

`second`'s `x` sits where `first`'s `secret` sat. Nothing in `second` is wrong on its face; the bug is the read of `x` before any write.

## Looking at raw memory: x

`print` shows a variable as its type. `x` (examine) shows memory as you ask:

```
(gdb) x/4dw &count        4 words (4 bytes each), as signed decimals, starting at &count
(gdb) x/8xb &count        8 bytes in hex
(gdb) x/2gx $sp           2 giant (8-byte) words in hex at the stack pointer
```

The format is `x/NFU ADDRESS`: N items, format F (`d` decimal, `x` hex, `u` unsigned, `c` char, `s` string), unit U (`b` byte, `h` 2 bytes, `w` 4 bytes, `g` 8 bytes). `ptype count` and `whatis count` tell you a variable's type. That matters when you read raw memory, because 4 bytes as an `int` and 4 bytes as a `float` are different numbers.

## Watchpoints: stop when a value changes

A breakpoint stops at a place. A **watchpoint** stops when a *value* changes, wherever that happens:

```
(gdb) watch total
Watchpoint 2: total
(gdb) continue
Watchpoint 2: total
Old value = 0
New value = 1299
sum (n=4) at shop.c:14
```

Each time `total` is written, gdb stops and shows the old and new value and the line that wrote it. `rwatch x` stops when `x` is *read*, and `awatch x` on either. `info watchpoints` lists them; `delete` removes them.

A watchpoint on a local only makes sense while its function is running: when the function returns, gdb deletes the watchpoint and tells you (`Watchpoint 2 deleted because the program has left the block`). Set it after you have stopped inside the function.

**The cost.** In these labs, watchpoints are *software* watchpoints (the lab's gdb settings turn hardware ones off, because the sandbox does not support them). gdb single-steps the program and checks the value after every instruction. That is thousands of times slower than running normally. Watch narrowly: stop near the interesting code first, then `watch`, then `continue`.

**The silence is the clue.** If you watch a variable and `continue`, and gdb says nothing until the function ends, nothing wrote that variable. When the variable is then used, it holds whatever was there before.

## until

`until` is `next` that will not go backwards: at the end of a loop body it runs the remaining iterations and stops at the first line *after* the loop. `until 30` runs until line 30.

## Making the missing write

When a variable was never set, you can set it yourself at the point where the program should have: stop at the start of the function, `set var count = 0`, and `continue`. If the program now behaves, the bug is the missing initialisation. In the source, that is a one-character fix.

## Try it

In this lab, a checkout program checks a discount code against a list of valid ones. It accepts a code that is not on the list. Find the variable that decides, see that nothing ever writes it for an invalid code, and give it the value it should have started with.

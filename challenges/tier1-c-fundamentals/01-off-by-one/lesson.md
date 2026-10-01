# Meet gdb: run, break, step, look

A program that prints the wrong answer gives you a symptom, not a cause. You can stare at the source and guess, or you can stop the program halfway and look at what it actually holds. A debugger lets you do the second: pause a running program at any line, read its variables, change them, and let it continue. This lesson covers the dozen commands you will use in every lab.

## What gdb needs

gdb reads the program's machine code and, if the program was compiled with `-g`, a map from that code back to your source: which instructions belong to which line, where each variable lives, what type it has. Without `-g` you can still debug, but you see addresses and registers instead of names. Every lab binary here is built with `-g` and without optimisation (`-O0`), so each source line maps to its own instructions and every variable is where the source says it is.

Start gdb with the program as its argument:

```
lab$ gdb ./vowels
(gdb)
```

`(gdb)` is the prompt. The program is loaded but not running yet.

## run, break, continue

`run` starts the program. On its own it simply runs to the end, exactly as it would from the shell. To stop somewhere, set a **breakpoint** first:

```
(gdb) break count_vowels
Breakpoint 1 at 0x401176: file vowels.c, line 9.
(gdb) run
Breakpoint 1, count_vowels (s=0x4020a0 "debugging") at vowels.c:9
9	    int count = 0;
```

You can break on a function name (`break count_vowels`), a line (`break 12`) or a file and line (`break vowels.c:12`). The program stops *before* executing the line shown. `continue` (or `c`) lets it run until the next breakpoint or the end.

## Looking around: list, print, info locals

- `list` prints the source around where you stopped. `list 1,30` prints lines 1 to 30.
- `print count` (or `p count`) prints a variable. It takes C expressions: `print s[2]`, `print count * 2`, `print len - 1`.
- `print arr[0]@5` prints five consecutive elements starting at `arr[0]`. The `@` is gdb's, not C's.
- `info locals` prints every local variable of the current function at once.

Variables you have not reached yet can hold anything. A local declared on line 9 is not meaningful until line 9 has run.

## next and step

Both run one source line and stop again. They differ when the line calls a function:

- `next` (`n`) runs the whole call and stops at the following line of *this* function.
- `step` (`s`) goes *into* the called function and stops at its first line.

Pressing Enter on an empty line repeats the last command, so stepping through a loop is `next`, then Enter, Enter, Enter.

## display: print every time you stop

Typing `print i` after every `next` gets old. `display i` prints `i` automatically each time the program stops:

```
(gdb) display i
1: i = 0
(gdb) next
11	        if (is_vowel(s[i]))
1: i = 0
(gdb) next
10	    for (int i = 0; s[i] != '\0'; i++)
1: i = 1
```

`display` takes any expression (`display s[i]`). `undisplay 1` removes display number 1.

## Changing the program while it runs: set var

gdb can write as well as read. `set var count = 4` changes `count` in the paused program, and when you `continue`, the program carries on with the new value. Nothing in the source changes, and nothing is recompiled. You are editing the running process's memory. In these labs there is no compiler and you cannot change the source, so this is how you test a theory: put the right value where the wrong one is and see whether the rest of the program behaves.

## finish

`finish` runs until the current function returns, stops in the caller and prints the return value:

```
(gdb) finish
Run till exit from #0  count_vowels (s=0x4020a0 "debugging") at vowels.c:13
0x0000000000401203 in main () at vowels.c:20
20	    int n = count_vowels(word);
Value returned is $1 = 3
```

The caller's line is only half done: the function has returned, but its value has not been stored in `n` yet. One `next` finishes the line.

## When it crashes

If the program reads memory it does not own, the kernel stops it with a signal and gdb tells you where:

```
Program received signal SIGSEGV, Segmentation fault.
0x0000000000401182 in count_vowels (s=0x0) at vowels.c:10
10	    for (int i = 0; s[i] != '\0'; i++)
```

Read it right to left: the function and its arguments (`s=0x0`, a null pointer), the file and line, then the line itself. The next lesson is about crashes.

## Two things you will see in these labs

- On every `run`, gdb prints `warning: Error disabling address space randomization: Invalid argument`. The sandbox the labs run in does not let gdb switch address randomisation off. Harmless: code and global variables are always at the same address, but the stack moves, so a local variable's address changes from one run to the next. Never rely on a stack address you wrote down during an earlier run.
- The labs print a `report:` line at the end. It is decoded from the program's state at that moment, and it only reads as a flag (`LAB{...}`) when that state is right.

## Try it

In this lab, a program adds up five exam scores and gets the total wrong. Find where the wrong number comes from, put the right total where the program uses it, and let it finish. You will need `break`, `run`, `next`, `display` and `print`, and `set var` for the fix.

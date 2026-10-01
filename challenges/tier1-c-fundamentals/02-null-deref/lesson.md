# Crashes and the stack

When a C program crashes, the line it died on is rarely where the mistake is. A bad value is usually made in one function, passed along, and only used, fatally, somewhere else. To get from the crash back to the cause you need to see the chain of calls that led there. That chain is the **stack**, and gdb lets you walk it.

## Frames

Every function call gets a **frame**: a block of memory holding its arguments, its local variables and the address to return to. When `main` calls `load_config`, which calls `parse_line`, three frames are live. `parse_line`'s is the newest. When `parse_line` returns, its frame is gone.

## bt: the first command after a crash

```
Program received signal SIGSEGV, Segmentation fault.
0x00000000004011a6 in apply (opt=0x0, value=8080) at config.c:31
31	    opt->value = value;
(gdb) bt
#0  0x00000000004011a6 in apply (opt=0x0, value=8080) at config.c:31
#1  0x0000000000401231 in parse_line (line=0x4020c8 "Port=8080") at config.c:44
#2  0x00000000004012b9 in main () at config.c:58
```

`bt` (backtrace) lists the frames, newest first. Frame #0 crashed: `apply` wrote through `opt`, and `opt` is `0x0`, a **null pointer**. Address zero is never mapped, so the kernel stopped the program with `SIGSEGV`. Frame #1 shows who called `apply`, with what, and from which line.

The crash is in `apply`, but `apply` did nothing wrong: it was handed a null pointer. The question is where that null came from.

## Moving between frames

- `frame 1` (or `up`, which moves one frame towards `main`) selects the caller. `down` moves back.
- Once a frame is selected, `list`, `print` and `info locals` work in *that* function: you can read `parse_line`'s variables even though the program stopped in `apply`.
- `info args` prints the selected frame's arguments. `info frame` prints its details: the caller, where its locals live, the saved return address.

```
(gdb) up
#1  0x0000000000401231 in parse_line (line=0x4020c8 "Port=8080") at config.c:44
44	        apply(find_option(key), atoi(val));
(gdb) print key
$1 = "Port"
```

`find_option(key)` returned null. `apply` crashed, but `find_option` is the suspect.

## Reading through pointers

`print opt->value` follows a pointer to a struct field, and `print *opt` prints the whole struct. If the pointer is null, gdb tells you it `Cannot access memory at address 0x0` instead of crashing. `print options[1]` prints an element of a global array of structs, and `print &options[1]` prints its address. A global's address is the same on every run.

## Conditional breakpoints

A loop that runs 500 times makes `break` + `continue` slow going. Add a condition:

```
(gdb) break config.c:22 if (int) strcmp(options[i].name, "port") == 0
```

The program stops at line 22 only when the condition is true. The condition can be any expression gdb can evaluate in that frame, including calls to functions in the program such as `strcmp`. Library functions have no debug information in these labs, so cast their result: `(int) strcmp(a, b) == 0`. `info breakpoints` lists breakpoints and their conditions; `condition 2` removes the condition from breakpoint 2; `delete 2` removes it altogether.

## finish and return

You met `finish`: run until the current function returns and show the value. Its sharper cousin is `return`, which makes the current function return *immediately*, without running the rest of it, optionally with a value you choose:

```
(gdb) return &options[1]
Make find_option return now? (y or n) y
```

The caller carries on as if `find_option` had found `options[1]`. These labs set `confirm off`, so the question is skipped. Use `return` with care: anything the function would have done after that point does not happen.

## Fixing state from the outside

Combine the two ideas: stop where the bad value arrives, replace it, carry on. If `apply` is about to receive a null `opt`, you can stop at the start of `apply` and `set var opt = &options[1]`, then `continue`. You have not changed the source. You have tested the theory "if this pointer were right, the rest would work". If the rest works, you have found the bug.

## Try it

In this lab, a program looks up a user by name and greets them. It crashes. Use `bt` to see where, move up the stack to see why, and give the greeting the user record it should have had.

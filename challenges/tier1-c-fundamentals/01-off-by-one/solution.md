# Off by one: walkthrough

**Symptom.** The five scores are 72, 85, 91, 64 and 88, which add up to 400 and average 80.0. The program prints `total of 5 scores: 437` and `average: 87.4`.

**Find it.** Stop in the function that adds them up and step through the loop:

```
(gdb) break sum_scores
(gdb) run
(gdb) display i
(gdb) display total
(gdb) next
...
```

Each pass adds `scores[i]` to `total`. The loop runs while `i <= n`, and `n` is 5, so it runs for `i` = 0, 1, 2, 3, 4 **and 5**. The array has five elements, `scores[0]` to `scores[4]`. `scores[5]` is one past the end:

```
(gdb) print scores[0]@5
$1 = {72, 85, 91, 64, 88}
(gdb) print scores[5]
$2 = 37
```

Reading past the end of an array is undefined behaviour in C. Here it reads whatever the compiler placed next in memory: the gradebook's `curve` field, 37. 400 + 37 = 437.

**Fix it in gdb.** Let `sum_scores` return, finish the assignment in `main`, then correct `total`:

```
(gdb) finish
(gdb) next
(gdb) print total
$3 = 437
(gdb) set var total = 72 + 85 + 91 + 64 + 88
(gdb) continue
total of 5 scores: 400
average: 80.0
report: LAB{...}
```

**The real fix** is in the source: the loop condition should be `i < n`. Loops over an array of `n` elements run from 0 while `i < n`; `<=` is the classic off-by-one.

# session.gdb — a realistic gdb workflow on /opt/perf/perf, one command per line.
# Phase 1: `gdb -batch -x session.gdb perf` must run it end to end (task 1.4).
# Phase 3/4: clients replay it line by line with pacing. Lines tagged "# profile: X" start the
# section a profile draws commands from (reader: looking around; stepper: next/step/print).
# Plain `#` lines are comments; blank lines are skipped. No `shell`, `python` or `pipe`.

# profile: setup
set width 200
break main
break sum_scores
break step_loop
run

# profile: reader
list
info locals
bt
info frame
info breakpoints
print scores
print accounts[1]
ptype struct account
print sizeof(struct account)
x/5dw scores

# profile: stepper
# Into sum_scores (breakpoint 2), where `i` exists. With a dynamic binary, `display i` in
# main silently bound to a global `i` in the musl loader; static binaries have none (ADR 0010).
continue
next
next
step
info args
display total
display i
next
next
next
next
next
print i
print scores[i]
print total
finish
undisplay
next
next
print total
set var total = 150
print total
next
next
next
next
print *find("bob")
print accounts[1].balance
next
next
next
x/16xb accounts[1].name
print/x accounts[1].checksum
next
next
next
next
next
next

# profile: threads
break threads_ready
continue
info threads
thread apply all bt
next
next

# profile: watch
continue
watch counter
continue
continue
continue
delete
finish

# profile: finish
tbreak report
continue
info args
call report(99)
print key
set var key = 42
continue

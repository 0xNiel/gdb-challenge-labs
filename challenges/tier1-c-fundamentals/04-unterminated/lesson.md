# Bytes, strings, and memory

A C string is not a type. It is a convention: a run of bytes that ends at the first byte equal to zero, the NUL (`'\0'`). Every string function, `strlen`, `printf("%s")`, `strcpy`, trusts that the NUL is there and keeps reading until it finds one. When it is missing, they read on into whatever memory follows, and the program carries on with a wrong answer and no error. This lesson teaches you to look at memory as the bytes it is.

## The same memory, three ways

Suppose `char host[8]` holds `"web01"`:

```
(gdb) print host
$1 = "web01\000\000"
(gdb) x/s host
0x4c6110 <host>:	"web01"
(gdb) x/8xb host
0x4c6110 <host>:	0x77	0x65	0x62	0x30	0x31	0x00	0x00	0x00
(gdb) x/8c host
0x4c6110 <host>:	119 'w'	101 'e'	98 'b'	48 '0'	49 '1'	0 '\000'	0 '\000'	0 '\000'
```

- `print` knows the array is 8 bytes and shows all of them, NULs as `\000`.
- `x/s` reads as a C string: from the address to the first NUL, however far that is. That is exactly what `strlen` and `printf` do, so `x/s` shows you what the program sees.
- `x/xb` and `x/c` show each byte: hex, or number and character.

`p/x value` prints any value in hex, `p/c 65` prints `'A'`, `p/d 'A'` prints 65. `sizeof` works in expressions: `print sizeof host` is 8, `print sizeof(struct entry)` is the size of the whole struct.

## Struct layout: what is next to what

Fields of a struct sit in memory in the order they are declared, sometimes with padding between them for alignment. `ptype /o` shows the offsets:

```
(gdb) ptype /o struct entry
/* offset      |    size */  type = struct entry {
/*      0      |       8 */    char host[8];
/*      8      |       4 */    int port;
/*     12      |       4 */    unsigned flags;
                               /* total size (bytes):   16 */
                             }
```

`host` takes bytes 0 to 7 and `port` starts at byte 8, right after `host`'s last byte. If `host` holds 8 characters and no NUL, a string function reading `host` goes straight on into `port`'s bytes.

## strncpy does not promise a NUL

`strncpy(dst, src, n)` copies at most `n` bytes. If `src` is shorter, it pads with NULs. If `src` has `n` characters or more, it copies `n` and **stops without a NUL**. `strncpy(host, "frontend", 8)` fills all 8 bytes of `host` with letters. The result is not a string.

## Writing bytes

`set var` writes a variable. To write raw memory, give gdb an address and a type:

```
(gdb) set {char}(host + 5) = 0       write one char at host + 5
(gdb) set var host[5] = 0            the same, through the array
(gdb) set {int}&port = 443           an int at &port
```

`{type}address` means "the `type` stored at `address`". With it you can put a NUL exactly where a string should end, even if that byte belongs to the next field.

## Registers

`info registers` lists the CPU's registers. On x86-64, `info registers rip rsp` shows the instruction pointer (the next instruction to run) and the stack pointer (the top of the stack). `x/8gx $rsp` examines the stack directly. You will use these in the next lesson. Code addresses such as `$rip` are the same on every run in these labs; stack addresses such as `$rsp` are not.

## Try it

In this lab, a program issues a badge for an employee whose name is exactly 8 characters long. The badge prints a longer name than it should, and the badge key comes out wrong. Look at the name as bytes, find where the string really ends, and end it where it should.

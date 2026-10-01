# Unterminated string: walkthrough

**Symptom.** The badge is for `Thompson` (8 characters), but the program prints `badge for Thompsonadmin (13 characters)` and a badge key computed from that.

**Find it.** Stop once the badge is filled in and look at the name as bytes:

```
(gdb) break badge_key
(gdb) run
(gdb) x/16xb card.name
<card>:     0x54 0x68 0x6f 0x6d 0x70 0x73 0x6f 0x6e
<card+8>:   0x61 0x64 0x6d 0x69 0x6e 0x00 0x00 0x00
(gdb) x/s card.name
<card>:	"Thompsonadmin"
(gdb) ptype /o struct badge
/* offset      |    size */  type = struct badge {
/*      0      |       8 */    char name[8];
/*      8      |       8 */    char role[8];
/*     16      |       4 */    unsigned int level;
```

`Thompson` fills all 8 bytes of `name`: `strncpy(b->name, name, 8)` copied 8 characters and no NUL. The next byte is `role[0]`, the `a` of `admin`, so `strlen(card.name)` reads `Thompsonadmin` and returns 13. The key mixes in that length.

**Fix it in gdb.** End the string after its 8 characters. That byte is `role[0]`:

```
(gdb) set var card.role[0] = 0
(gdb) x/s card.name
<card>:	"Thompson"
(gdb) continue
report: LAB{...}
```

`set {char}(card.name + 8) = 0` writes the same byte by address.

**The real fix:** make room for the terminator (`char name[9]`) and terminate explicitly after `strncpy` (`b->name[sizeof b->name - 1] = 0`), or use `snprintf`, which always terminates.

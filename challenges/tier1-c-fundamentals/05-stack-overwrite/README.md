# packet

`packet` receives one network packet, computes its checksum, stores the packet in a receive frame, and checks the checksum again before handing the packet on. A packet that arrived intact is accepted and followed by a report line.

```
./packet          # run it
gdb ./packet      # debug it
```

The source is in `src/main.c`. This is the tier's last lab: no hints.

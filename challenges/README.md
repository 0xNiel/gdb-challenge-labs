# challenges

Curriculum content. One directory per challenge: `tierN-<slug>/NN-<slug>/`. The manifest slug is `tierN-NN-<slug>` and doubles as the image name.

```
tier1-c-fundamentals/01-off-by-one/
  manifest.yaml     # schema/manifest.schema.json; `image:` written by the build
  lesson.md         # rendered by web before the lab
  README.md         # copied INTO the lab image (/opt/lab/README.md): what the program should do
  src/main.c        # the buggy program, < 120 lines, one bug
  build.sh          # exact flags; always includes -no-pie -fno-pie
  solve.gdb         # PRIVATE oracle; never copied into the image
  solution.md       # PRIVATE walkthrough shown after a solve
```

`schema/` holds `manifest.schema.json`, `challenges.schema.json`, `flag_vectors.json` and `ws_token_vectors.json` — the contracts shared between Go and Python.

The full authoring guide (design rules, hint ladder, how `report()` and the flag blob work, how to run the oracle) is written in Phase 5, task 5.14, and replaces this stub. Tiers 2 and 3 are authored in Phase 8.

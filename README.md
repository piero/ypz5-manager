# Samsung YP-Z5 File Manager

TUI for managing files on FAT32 partition,
initially targeted at the Samsung YP-Z5 player.


## Application structure

```
┌─────────────────┐
│   Bubble Tea    │
│      TUI        │
└────────┬────────┘
         │
         │ commands
         ▼
┌─────────────────┐
│   application   │
│     model       │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   FAT32 layer   │
│                 │
│ list            │
│ read            │
│ delete          │
│ mkdir           │
│ etc.            │
└────────┬────────┘
         │
         ▼
   /dev/rdisk4s1
```

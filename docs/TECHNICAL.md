# Technical overview

`ypz5-manager` reads and writes the FAT32 filesystem directly, through its own
FAT32 implementation, instead of going through the operating system's
driver. This gives full control over what is written to the device, and
lets the same code run against a disk image or a raw device node.

## Application structure

```
main.go                  flags; opens the storage and the filesystem; starts the TUI
cmd/inspect/             read-only inspection tool
internal/tui/            Bubble Tea screens
  model.go                 state, dispatch to the active screen, scrolling
  browser.go               YP-Z5 browser and file details
  local.go                 "add files from this Mac" screen
  local_browser.go         local folder listing and selection
  picker.go                destination picker
  copy.go                  copy progress and summary
  confirm.go               device-write and delete confirmations
  scroll.go, theme.go      list scrolling; color palettes
internal/transfer/       copy planning and execution (Mac → YP-Z5), no UI
internal/storage/        image/device access: partition offset, flush, mount guard
internal/testimage/      test helper: writable clone of the YP-Z5 image
fat32/                   the FAT32 filesystem (see `go doc ./fat32`)
```

## Layers

```
┌─────────────────┐      ┌──────────────────┐
│  internal/tui   │─────▶│ internal/transfer│
└────────┬────────┘      └────────┬─────────┘
         │                        │
         ▼                        ▼
┌──────────────────────────────────────────┐
│                 fat32                    │
└────────────────────┬─────────────────────┘
                     │ io.ReaderAt / io.WriterAt
┌────────────────────▼─────────────────────┐
│             internal/storage             │
└────────────────────┬─────────────────────┘
                     ▼
        disk image  or  /dev/rdisk4
```

- **`internal/tui`** holds only presentation and input handling.
- **`internal/transfer`** plans and runs copies from the Mac to the
  device, with no UI: it filters out `.DS_Store` and `._*` files, skips
  existing files, merges existing folders, and checks free space before
  writing anything.
- **`fat32`** knows nothing about devices or files on disk: it works on any
  `io.ReaderAt` / `io.WriterAt`.
- **`internal/storage`** opens an image or device, locates the FAT32
  partition (a whole disk with an MBR or a bare partition both work),
  flushes writes, and refuses to open a device for writing while any of
  its partitions is mounted.

## The FAT32 package

The `fat32` package lets callers navigate the filesystem without knowing
anything about physical sectors. Each level is built on the one above it:

```
boot sector
    ↓
FAT layout
    ↓
FAT entries
    ↓
cluster chain
    ↓
directory entries / LFN
    ↓
file contents
```

| File                  | Responsibility                                              |
|-----------------------|-------------------------------------------------------------|
| `filesystem.go`       | `Filesystem`, `Open`, flushing (commit)                     |
| `operations.go`       | public mutations: `WriteFile`, `ReplaceFile`, `Mkdir`, `Delete`, `RemoveAll` |
| `path.go`             | path resolution, `ErrNotFound`                              |
| `directory.go`        | reading directories                                         |
| `directory_write.go`  | adding, growing and deleting directory records              |
| `entry.go`            | the 32-byte directory entry, attributes                     |
| `shortname.go`        | 8.3 names and NT case flags                                 |
| `lfn.go`              | long file names                                             |
| `timestamp.go`        | FAT dates and times                                         |
| `fat.go`              | the in-memory FAT, allocation and cluster chains            |
| `cluster.go`          | reading and writing data clusters                           |
| `file.go`             | reading and writing file contents                           |
| `fsinfo.go`           | the FSInfo free-cluster count                               |
| `boot_sector.go`, `layout.go` | boot sector and derived geometry                    |
| `partition.go`        | finding the filesystem inside an MBR disk                   |

Run `go doc ./fat32` for the full API.

## Write safety

Every top-level mutation (`WriteFile`, `ReplaceFile`, `Mkdir`, `Delete`,
`RemoveAll`) is ordered so that a crash or failed write can only **leak**
clusters (leave them marked in use with nothing pointing to them), never
**cross-link** them. Each mutation ends by updating FSInfo and flushing the
writer.

In practice, unplugging the player mid-operation costs at most some free
space, which `fsck_msdos` can reclaim; the filesystem itself stays
consistent.

Additional guards:

- The storage is opened read-only unless `--write` is given, and writing
  to a device asks for confirmation.
- Writing is refused while any partition of the target disk is mounted,
  since two writers would corrupt the filesystem.
- Read-only and system entries (`System`, `DevIcon.fil`, `DevLogo.fil`…)
  are reported as protected and cannot be deleted (`ErrProtected`).
- Existing files are never overwritten by a copy.

## The YP-Z5 disk

The player exposes a single disk of about 2.0 GB with an MBR; the FAT32
partition starts at sector 32. After files are added, the player rebuilds
its music library from the `Music` folder on its own, so `ypz5-manager` only needs
to place files there.

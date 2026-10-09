# Samsung YP-Z5 File Manager

TUI for managing files on FAT32 partition,
initially targeted at the Samsung YP-Z5 player.


## Running the TUI

The tool works on either a disk image or the real device. It opens
read-only unless `--write` is given.

```sh
go run . --image PATH  [--write]     # a disk image file
go run . --device PATH [--write]     # a device node, e.g. /dev/rdisk4
```

Either a whole disk (with its MBR) or a single partition works: the FAT32
partition is found automatically.

Screens are color-coded by the filesystem they operate on: **blue/cyan**
for the YP-Z5 (device or image), **amber/orange** for the Mac's local
files. Each screen also starts with a ` YP-Z5 ` or ` MAC ` tag. Set
`NO_COLOR=1` to disable colors.

### Keys

| Screen          | Keys                                                                 |
|-----------------|----------------------------------------------------------------------|
| Browser         | `↑`/`↓` or `k`/`j` move, `PgUp`/`PgDn`/`Home`/`End` scroll, `Enter` open, `Backspace`/`h` up, `q` quit |
| Browser (write) | `n` add files from the Mac, `d` delete (confirm with `y`)            |
| Add files       | `↑`/`↓`, `PgUp`/`PgDn`/`Home`/`End` as above, `Space` select, `Enter` open folder, `Backspace`/`h` up, `t` change destination, `c` copy, `Esc` back |
| Destination     | Browse device folders as above, `s` copy into the current folder, `Esc` cancel |
| Copying         | `Esc` cancels after the current file                                 |

Files are copied into `/Music` by default; press `t` on the add-files
screen to pick another device folder.
`.DS_Store` and `._*` files are never copied, existing files are skipped
(never overwritten), and existing folders are merged into. Free space is
checked before anything is written.

Entries marked `(protected)` (read-only or system, such as `System`,
`DevIcon.fil` and `DevLogo.fil`) cannot be deleted.

### On a disk image

Reading is always safe:

```sh
go run . --image images/yp-z5a-full-20261009.img
```

To try writing, use a copy so the pristine image is never modified
(`cp -c` makes an instant APFS clone):

```sh
cp -c images/yp-z5a-full-20261009.img /tmp/scratch.img
chmod u+w /tmp/scratch.img
go run . --image /tmp/scratch.img --write
```

Check the result afterwards:

```sh
go run ./cmd/inspect -path /Music /tmp/scratch.img
```

and with macOS's own checker. `fsck_msdos` reads a bare partition
directly, but a full disk image (with its MBR) must be attached first,
without mounting it:

```sh
hdiutil attach -nomount -imagekey diskimage-class=CRawDiskImage /tmp/scratch.img
# prints e.g. /dev/disk7 and /dev/disk7s1
fsck_msdos -n /dev/disk7s1
hdiutil detach /dev/disk7
```

### On the real device

Before writing to the player, make sure a recent backup image exists
(see [Creating an image from the device](#creating-an-image-from-the-device)).

1. Connect the player and find its disk (about 2.0 GB, named `YP-Z5`):

   ```sh
   diskutil list
   ```

   The examples below use `disk4`. **Check the number every time**: it
   can change between connections.

2. Unmount it. macOS mounts the player automatically, and the tool
   refuses to write while any part of the disk is mounted, because two
   writers would corrupt the filesystem:

   ```sh
   diskutil unmountDisk /dev/disk4
   ```

3. Run the tool. Device nodes need `sudo`; use the raw node `rdisk`,
   which is much faster:

   ```sh
   go build -o ypz5 .
   sudo ./ypz5 --device /dev/rdisk4            # read-only
   sudo ./ypz5 --device /dev/rdisk4 --write    # read/write, asks to confirm
   ```

   Every operation is flushed to the device when it finishes. Don't
   unplug the player while a copy or delete is running; if it happens,
   the worst case is lost free space (clusters still marked in use), not
   a corrupted filesystem.

4. Optionally check the filesystem, then eject:

   ```sh
   sudo fsck_msdos -n /dev/rdisk4s1
   sudo go run ./cmd/inspect -path /Music /dev/rdisk4
   diskutil eject /dev/disk4
   ```

5. Unplug the player and let it rebuild its library from `Music`.

### Restoring a backup

If anything goes wrong, write the backup image back to the device. This
overwrites the whole disk: double-check the disk number first.

```sh
diskutil unmountDisk /dev/disk4
sudo dd if=images/yp-z5a-full-20261009.img of=/dev/rdisk4 bs=1m status=progress
diskutil eject /dev/disk4
```

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

The FAT32 package allows navigating the filesystem without knowing anything about physical sectors:

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


## Disk images

Tests and experiments should run against an image of the device, never
the device itself.

### Creating an image from the device

With the YP-Z5 connected:

```sh
diskutil list                       # find the ~2.0 GB YP-Z5 disk, e.g. /dev/disk4
diskutil unmountDisk /dev/disk4     # nothing may be mounted while dumping
sudo dd if=/dev/rdisk4 of=images/yp-z5a-full-$(date +%Y%m%d).img bs=1m status=progress
sudo chown $USER images/yp-z5a-full-*.img
diskutil eject /dev/disk4
```

`/dev/rdisk4` is the raw (unbuffered) device node, which is much faster
than `/dev/disk4`. The dump includes the MBR; the FAT32 partition starts
at sector 32.

### Checksum and write protection

Record a checksum, then make the image read-only so it stays pristine:

```sh
shasum -a 256 images/yp-z5a-full-20261009.img > images/yp-z5a-full-20261009.img.sha256
chmod a-w images/yp-z5a-full-20261009.img
```

To verify it later:

```sh
shasum -a 256 -c images/yp-z5a-full-20261009.img.sha256
```

To experiment with the TUI in read/write mode, work on a copy (see
[On a disk image](#on-a-disk-image)).


## Running the tests

Unit tests need no setup:

```sh
go test ./...
```

The YP-Z5 integration tests are skipped unless `YP_Z5_IMAGE` points at a
full device image. Run from the repository root:

```sh
YP_Z5_IMAGE="$PWD/images/yp-z5a-full-20261009.img" go test ./...
```

- The path must be absolute (hence `$PWD`): `go test` runs each package
  from its own directory, so a relative path would not be found.
- The image may be read-only. Each mutating test works on its own copy
  (an APFS clone on macOS) in a temporary directory, and checks that the
  filesystem is still consistent afterwards.
- The tests assume the partition starts at byte 16384 and is
  2,028,355,584 bytes long (`fat32/filesystem_ypz5_test.go`).
- Some tests expect specific files on the device, e.g.
  `Music/Cartoni animati/Pollon.mp3`.

Useful variations:

```sh
# only the YP-Z5 tests, verbose
YP_Z5_IMAGE="$PWD/images/yp-z5a-full-20261009.img" go test ./fat32 -run YPZ5 -v

# ignore cached results, e.g. after replacing the image
YP_Z5_IMAGE="$PWD/images/yp-z5a-full-20261009.img" go test -count=1 ./...
```


## Inspecting an image or device

`cmd/inspect` prints the filesystem geometry, the free space recorded in
FSInfo and a detailed listing of one directory (attributes, modification
date, 8.3 name, size, first cluster). It only reads.

```sh
go run ./cmd/inspect -path /Music images/yp-z5a-full-20261009.img
sudo go run ./cmd/inspect -path /Music /dev/rdisk4    # device, unmounted
```

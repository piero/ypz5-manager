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

To experiment with the TUI in read/write mode, work on a copy:

```sh
cp -c images/yp-z5a-full-20261009.img /tmp/scratch.img && chmod u+w /tmp/scratch.img
go run . --image /tmp/scratch.img --write
```


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

# Development guide

## Prerequisites

- [Go](https://go.dev/dl/) 1.27 or later
- macOS is the primary platform (device mount detection and APFS clones
  are macOS-specific); other platforms can work with disk images.
- Optional: [mtools](https://www.gnu.org/software/mtools/) to create small
  test images without a device.

```sh
go run . --image PATH [--write]
go build -o ypz5-manager .
```

See [TECHNICAL.md](TECHNICAL.md) for how the code is organized.

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

### Creating a test image without a device

Any consistent FAT32 image works. With mtools:

```sh
mformat -C -F -T 163840 -h 16 -s 32 -c 1 -i /tmp/test.img ::
mcopy -s -i /tmp/test.img ~/Music/SomeAlbum ::/
```

## Trying the TUI on an image

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

### Checking the result

With the bundled inspection tool:

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

On the real device (unmounted):

```sh
sudo fsck_msdos -n /dev/rdisk4s1
```

## Inspecting an image or device

`cmd/inspect` prints the filesystem geometry, the free space recorded in
FSInfo and a detailed listing of one directory (attributes, modification
date, 8.3 name, size, first cluster). It only reads.

```sh
go run ./cmd/inspect -path /Music images/yp-z5a-full-20261009.img
sudo go run ./cmd/inspect -path /Music /dev/rdisk4    # device, unmounted
```

## Running the tests

Unit tests need no setup:

```sh
go test ./...
```

The integration tests are skipped unless `YP_Z5_IMAGE` points at a disk
image. Run from the repository root:

```sh
YP_Z5_IMAGE="$PWD/images/yp-z5a-full-20261009.img" go test ./...
```

- Any consistent FAT32 image works, not only a YP-Z5 dump: a whole disk
  with an MBR or a bare partition. The partition is found automatically,
  the tests create the files and folders they need (at the root, named
  `YPZ5 …`), and tests that need existing files use whatever the image
  contains, skipping when it has none.
- The path must be absolute (hence `$PWD`): `go test` runs each package
  from its own directory, so a relative path would not be found.
- The image may be read-only and is never modified. Each mutating test
  works on its own copy (an APFS clone on macOS) in a temporary
  directory, and checks that the filesystem is still consistent
  afterwards.
- If the image itself is damaged (lost or cross-linked clusters),
  `TestYPZ5ImageIsConsistent` fails and the mutating tests are skipped:
  repair the image first, e.g. with `fsck_msdos`.

Useful variations:

```sh
# only the image tests in fat32, verbose
YP_Z5_IMAGE="$PWD/images/yp-z5a-full-20261009.img" go test ./fat32 -run YPZ5 -v

# ignore cached results, e.g. after replacing the image
YP_Z5_IMAGE="$PWD/images/yp-z5a-full-20261009.img" go test -count=1 ./...
```

## Contributing

Contributions are welcome. Before opening a pull request:

1. Run `gofmt` and `go vet ./...`.
2. Run the full test suite, ideally with `YP_Z5_IMAGE` set so the
   integration tests run too.
3. For changes to `fat32`, keep the write-ordering guarantee described in
   [TECHNICAL.md](TECHNICAL.md#write-safety): a failed write may leak
   clusters but must never cross-link them. Add a test that checks the
   filesystem is still consistent afterwards.
4. Never commit disk images: they contain personal data from the device.

package transfer

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/piero/ypz5-manager/internal/testimage"
)

func TestPlanCopySkipsMacOSMetadata(t *testing.T) {
	root := t.TempDir()

	testimage.WriteTree(t, root, map[string]string{
		"Album/01 - Intro.mp3":   "intro",
		"Album/02 - Song.mp3":    "song!",
		"Album/.DS_Store":        "junk",
		"Album/._01 - Intro.mp3": "junk",
		"Single.mp3":             "single",
	})

	plan, err := PlanCopy(
		[]string{filepath.Join(root, "Album"), filepath.Join(root, "Single.mp3")},
		"/Music",
		4096,
	)
	if err != nil {
		t.Fatal(err)
	}

	var dests []string

	for _, op := range plan.Ops {
		dests = append(dests, op.Dest)
	}

	want := []string{
		"/Music/Album",
		"/Music/Album/01 - Intro.mp3",
		"/Music/Album/02 - Song.mp3",
		"/Music/Single.mp3",
	}

	if strings.Join(dests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ops:\n%s\nwant:\n%s", strings.Join(dests, "\n"), strings.Join(want, "\n"))
	}

	if !plan.Ops[0].IsDir {
		t.Fatal("first op should create the directory")
	}

	if plan.Files != 3 || plan.Bytes != int64(len("intro")+len("song!")+len("single")) {
		t.Fatalf("files = %d, bytes = %d", plan.Files, plan.Bytes)
	}

	if len(plan.Skipped) != 2 {
		t.Fatalf("skipped = %v, want .DS_Store and ._ file", plan.Skipped)
	}

	// 1 for the destination, 2 for the new directory, 1 per file.
	if plan.Clusters != 6 {
		t.Fatalf("clusters = %d, want 6", plan.Clusters)
	}
}

func TestPlanCopyAtRoot(t *testing.T) {
	root := t.TempDir()
	testimage.WriteTree(t, root, map[string]string{"a.mp3": "a"})

	plan, err := PlanCopy([]string{filepath.Join(root, "a.mp3")}, "/", 4096)
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Ops) != 1 || plan.Ops[0].Dest != "/a.mp3" {
		t.Fatalf("ops = %+v", plan.Ops)
	}
}

func TestCopyToImage(t *testing.T) {
	fs := testimage.Open(t)

	root := t.TempDir()
	big := strings.Repeat("x", 3*1024*1024+17)

	testimage.WriteTree(t, root, map[string]string{
		"New Album/01 - First.mp3":  big,
		"New Album/02 - Second.mp3": "second",
		"New Album/.DS_Store":       "junk",
	})

	const dest = "/YPZ5 Transfer Test"

	if err := fs.Mkdir(dest); err != nil {
		t.Fatal(err)
	}

	freeBefore, _ := fs.FreeClusters()

	plan, err := PlanCopy([]string{filepath.Join(root, "New Album")}, dest, fs.BytesPerCluster())
	if err != nil {
		t.Fatal(err)
	}

	if err := CheckSpace(fs, plan); err != nil {
		t.Fatal(err)
	}

	for _, op := range plan.Ops {
		skipped, err := Run(fs, op)
		if err != nil {
			t.Fatalf("%s: %v", op.Dest, err)
		}

		if skipped != "" {
			t.Fatalf("unexpectedly skipped %s", skipped)
		}
	}

	got, err := fs.ReadFile(dest + "/New Album/01 - First.mp3")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte(big)) {
		t.Fatal("copied file contents differ")
	}

	if _, err := fs.Lookup(dest + "/New Album/.DS_Store"); err == nil {
		t.Fatal(".DS_Store was copied")
	}

	freeAfter, _ := fs.FreeClusters()
	if used := int64(freeBefore - freeAfter); used > plan.Clusters {
		t.Fatalf("copy used %d clusters, plan estimated %d", used, plan.Clusters)
	}

	// Copying again skips existing files and merges into the directory.
	for _, op := range plan.Ops {
		skipped, err := Run(fs, op)
		if err != nil {
			t.Fatalf("second copy of %s: %v", op.Dest, err)
		}

		if !op.IsDir && skipped == "" {
			t.Fatalf("second copy of %s was not skipped", op.Dest)
		}
	}
}

func TestCheckSpaceRejectsOversizedCopy(t *testing.T) {
	fs := testimage.Open(t)

	free, _ := fs.FreeClusters()

	plan := Plan{Clusters: int64(free) + 1}

	if err := CheckSpace(fs, plan); err == nil {
		t.Fatal("checkSpace accepted a copy larger than the free space")
	}
}

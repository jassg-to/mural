package main

import (
	"strings"
	"testing"
)

func TestParseMountinfo(t *testing.T) {
	// Realistic mountinfo lines: a root mount (zero optional fields), a
	// mount with several optional fields, one with an escaped space in its
	// mount point, and a truncated line that must be skipped rather than
	// erroring.
	sample := strings.Join([]string{
		`36 25 0:27 / / rw,relatime shared:1 - ext4 /dev/root rw`,
		`43 36 8:1 / /boot rw,relatime shared:2 master:3 - vfat /dev/sda1 rw`,
		`61 36 8:17 / /media/mural/MY\040STICK ro,nosuid,nodev,noexec - vfat /dev/sdb1 ro,uid=1000,gid=1000`,
		`too few fields`,
		``,
	}, "\n")

	got, err := parseMountinfo(strings.NewReader(sample))
	if err != nil {
		t.Fatalf("parseMountinfo: %v", err)
	}
	want := []string{"/", "/boot", "/media/mural/MY STICK"}
	if len(got) != len(want) {
		t.Fatalf("got %d mount points %v, want %d %v", len(got), got, len(want), want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("mount point %d = %q, want %q", i, got[i], w)
		}
	}
}

func TestNewMountPoints(t *testing.T) {
	t.Run("first observation, prev empty", func(t *testing.T) {
		cur := map[string]bool{"/media/mural/a": true}
		got := newMountPoints(nil, cur)
		if len(got) != 1 || got[0] != "/media/mural/a" {
			t.Errorf("got %v, want [/media/mural/a]", got)
		}
	})

	t.Run("no change", func(t *testing.T) {
		prev := map[string]bool{"/media/mural/a": true}
		cur := map[string]bool{"/media/mural/a": true}
		got := newMountPoints(prev, cur)
		if len(got) != 0 {
			t.Errorf("got %v, want none", got)
		}
	})

	t.Run("addition", func(t *testing.T) {
		prev := map[string]bool{"/media/mural/a": true}
		cur := map[string]bool{"/media/mural/a": true, "/media/mural/b": true}
		got := newMountPoints(prev, cur)
		if len(got) != 1 || got[0] != "/media/mural/b" {
			t.Errorf("got %v, want [/media/mural/b]", got)
		}
	})

	t.Run("removal then re-addition", func(t *testing.T) {
		prev := map[string]bool{"/media/mural/a": true}
		cur := map[string]bool{}
		if got := newMountPoints(prev, cur); len(got) != 0 {
			t.Errorf("removal: got %v, want none", got)
		}
		// once dropped from the tracked set, re-adding is observed as new again
		reAdded := newMountPoints(cur, prev)
		if len(reAdded) != 1 || reAdded[0] != "/media/mural/a" {
			t.Errorf("re-addition: got %v, want [/media/mural/a]", reAdded)
		}
	})
}

func TestUnderRoot(t *testing.T) {
	tests := []struct {
		name       string
		mountPoint string
		root       string
		want       bool
	}{
		{"direct child", "/media/mural/stick", "/media/mural", true},
		{"root itself", "/media/mural", "/media/mural", false},
		{"sibling-prefix sibling directory", "/media/mural-backup/x", "/media/mural", false},
		{"trailing slash on root", "/media/mural/stick", "/media/mural/", true},
		{"trailing slash on both", "/media/mural/stick/", "/media/mural/", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := underRoot(tt.mountPoint, tt.root); got != tt.want {
				t.Errorf("underRoot(%q, %q) = %v, want %v", tt.mountPoint, tt.root, got, tt.want)
			}
		})
	}
}

package device

import "testing"

func TestParseMountInfo(t *testing.T) {
	tests := []struct {
		line                       string
		mountPoint, fsType, source string
		ok                         bool
	}{
		{
			line:       `412 30 8:17 / /run/media/me/GARMIN rw,nosuid,nodev,relatime shared:225 - vfat /dev/sdb1 rw,uid=1000`,
			mountPoint: "/run/media/me/GARMIN", fsType: "vfat", source: "/dev/sdb1", ok: true,
		},
		{
			// the optional fields vary, the separator ends them
			line:       `36 35 98:0 / /media/my\040watch rw - exfat /dev/sdc rw`,
			mountPoint: "/media/my watch", fsType: "exfat", source: "/dev/sdc", ok: true,
		},
		{
			line:       `61 30 0:52 / /run/user/1000/gvfs rw,nosuid,nodev,relatime shared:150 - fuse.gvfsd-fuse gvfsd-fuse rw`,
			mountPoint: "/run/user/1000/gvfs", fsType: "fuse.gvfsd-fuse", source: "gvfsd-fuse", ok: true,
		},
		{line: `garbage`},
	}
	for _, tt := range tests {
		mountPoint, fsType, source, ok := parseMountInfo(tt.line)
		if mountPoint != tt.mountPoint || fsType != tt.fsType || source != tt.source || ok != tt.ok {
			t.Errorf("%q: got %q %q %q %v", tt.line, mountPoint, fsType, source, ok)
		}
	}
}

func TestUnescapeMount(t *testing.T) {
	for in, want := range map[string]string{
		`/a\040b`:     "/a b",
		`/tab\011`:    "/tab\t",
		`/back\134x`:  `/back\x`,
		`/short\04`:   `/short\04`,
		`/not\999oct`: `/not\999oct`,
	} {
		if got := unescapeMount(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestMountRootsRuns(t *testing.T) {
	// the mounts of the machine running the tests: only checks mountinfo is read
	if _, err := mountRoots(); err != nil {
		t.Fatal(err)
	}
}

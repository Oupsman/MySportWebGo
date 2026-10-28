package device

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// massStorageTypes are the file systems of the devices: FAT for the watches, the others for SD cards
var massStorageTypes = map[string]bool{
	"vfat": true, "msdos": true, "exfat": true, "fuseblk": true, "ntfs": true, "ntfs3": true,
}

// mountRoots returns the mounted volumes of mountinfo, and the MTP devices mounted by GVfs: when GVfs
// holds an MTP device, libmtp can't open it, GVfs is then the only way to read it
func mountRoots() ([]root, error) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var roots []root
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		mountPoint, fsType, source, ok := parseMountInfo(scanner.Text())
		if !ok {
			continue
		}
		switch {
		case massStorageTypes[fsType]:
			roots = append(roots, root{
				Path:  mountPoint,
				ID:    volumeID(source, mountPoint),
				Label: filepath.Base(mountPoint),
				Kind:  MassStorage,
			})
		case fsType == "fuse.gvfsd-fuse":
			roots = append(roots, gvfsMTPRoots(mountPoint)...)
		}
	}
	return roots, scanner.Err()
}

// parseMountInfo returns the mount point, the file system type and the source of a line of mountinfo:
// "36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue"
func parseMountInfo(line string) (mountPoint, fsType, source string, ok bool) {
	before, after, found := strings.Cut(line, " - ")
	if !found {
		return "", "", "", false
	}
	fields, tail := strings.Fields(before), strings.Fields(after)
	if len(fields) < 5 || len(tail) < 2 {
		return "", "", "", false
	}
	return unescapeMount(fields[4]), tail[0], unescapeMount(tail[1]), true
}

// unescapeMount decodes the octal escapes of mountinfo (\040 for a space)
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if c, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(c))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// volumeID returns the file system UUID of a block device, the mount point when it has none
func volumeID(source, mountPoint string) string {
	if source, err := filepath.EvalSymlinks(source); err == nil {
		entries, _ := os.ReadDir("/dev/disk/by-uuid")
		for _, entry := range entries {
			target, err := filepath.EvalSymlinks(filepath.Join("/dev/disk/by-uuid", entry.Name()))
			if err == nil && target == source {
				return "volume:" + entry.Name()
			}
		}
	}
	return "path:" + mountPoint
}

// gvfsMTPRoots returns the MTP devices of a GVfs mount, named mtp:host=<vendor>_<product>_<serial>
func gvfsMTPRoots(gvfs string) []root {
	entries, err := os.ReadDir(gvfs)
	if err != nil {
		return nil
	}
	var roots []root
	for _, entry := range entries {
		host, ok := strings.CutPrefix(entry.Name(), "mtp:host=")
		if !ok {
			continue
		}
		roots = append(roots, root{
			Path:     filepath.Join(gvfs, entry.Name()),
			ID:       "mtp:" + host,
			Label:    host,
			Kind:     MTP,
			Storages: true,
		})
	}
	return roots
}

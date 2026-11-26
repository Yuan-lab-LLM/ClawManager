package workspace

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"io"
	"io/fs"
	"os"
	"path"
	"runtime"
)

// Verify reads an authenticated, unmodified data archive and an independently
// mounted read-only target. It never extracts, repairs, deletes or starts files.
// C shared volumes use the ordinary layout; Runtime-specific normalized roots
// need their owning B policy and must not be passed to this verifier.
func Verify(ctx context.Context, root *os.Root, src io.Reader, limits Limits, preserveOwner bool) (Report, error) {
	var report Report
	if root == nil || limits.MaxFiles <= 0 || limits.MaxBytes <= 0 {
		return report, ErrArchive
	}
	seen := map[string]bool{}
	tr := tar.NewReader(&contextReader{ctx, src})
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return report, ErrArchive
		}
		special := h.Mode & 07000
		sharedDirectory := h.Typeflag == tar.TypeDir && special == 02000 && preserveOwner && runtime.GOOS == "linux"
		if !(Layout("")).entry(h) || !(Layout("")).pax(h) || seen[h.Name] || h.Size < 0 || h.Uid < 0 || h.Gid < 0 || (special != 0 && !sharedDirectory) || (h.Name == "." && h.Typeflag != tar.TypeDir) || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA && h.Size != 0) {
			return report, ErrArchive
		}
		seen[h.Name] = true
		report.Files++
		if h.Size > limits.MaxBytes-report.Bytes {
			return report, ErrArchive
		}
		report.Bytes += h.Size
		if report.Files > limits.MaxFiles {
			return report, ErrArchive
		}
		// An archive path must not resolve through any target link, even a
		// confined alias to another file with identical content.
		for parent := path.Dir(h.Name); parent != "."; parent = path.Dir(parent) {
			info, e := root.Lstat(parent)
			if e != nil || !info.IsDir() {
				return report, ErrArchive
			}
		}
		info, e := root.Lstat(h.Name)
		if e != nil {
			return report, ErrArchive
		}
		// Reject unsupported target ACLs/capabilities/mount crossings as the
		// original ordinary-layout collector does; do not bless extra authority.
		if _, e := archiveMetadata(root, h.Name, ""); e != nil {
			return report, ErrArchive
		}
		actual, e := tar.FileInfoHeader(info, "")
		if e != nil || actual.Mode != h.Mode {
			return report, ErrArchive
		}
		if preserveOwner && (actual.Uid != h.Uid || actual.Gid != h.Gid) {
			return report, ErrArchive
		}
		if (info.IsDir() || info.Mode().IsRegular()) && !info.ModTime().Equal(h.ModTime) {
			return report, ErrArchive
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if !info.IsDir() {
				return report, ErrArchive
			}
		case tar.TypeSymlink:
			link, e := root.Readlink(h.Name)
			if e != nil || info.Mode()&os.ModeSymlink == 0 || link != h.Linkname {
				return report, ErrArchive
			}
		case tar.TypeReg, tar.TypeRegA:
			if !info.Mode().IsRegular() || info.Size() != h.Size {
				return report, ErrArchive
			}
			f, e := root.Open(h.Name)
			if e != nil {
				return report, ErrArchive
			}
			opened, e := f.Stat()
			if e != nil || !os.SameFile(info, opened) {
				f.Close()
				return report, ErrArchive
			}
			archiveHash, targetHash := sha256.New(), sha256.New()
			_, ae := io.CopyN(archiveHash, tr, h.Size)
			_, te := io.Copy(targetHash, io.LimitReader(&contextReader{ctx, f}, h.Size+1))
			after, se := f.Stat()
			ce := f.Close()
			if ae != nil || te != nil || se != nil || ce != nil || !os.SameFile(info, after) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || string(archiveHash.Sum(nil)) != string(targetHash.Sum(nil)) {
				return report, ErrArchive
			}
		default:
			return report, ErrArchive
		}
	}
	count := 0
	err := fs.WalkDir(root.FS(), ".", func(name string, _ fs.DirEntry, e error) error {
		if e != nil || !seen[name] {
			return ErrArchive
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		count++
		return nil
	})
	if err != nil || count != len(seen) {
		return report, ErrArchive
	}
	return report, nil
}

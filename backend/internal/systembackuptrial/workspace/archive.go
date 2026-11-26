// Package workspace transports entire registered instance roots, regardless of
// agent type. Callers supply a quiesced/snapshot root and an encrypted sink.
package workspace

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"runtime"
	"sort"
	"strings"
)

var ErrArchive = errors.New("workspace archive is unsafe, incomplete or exceeds capacity")

type Limits struct{ MaxFiles, MaxBytes int64 }
type Report struct{ Files, Bytes int64 }

func validName(s string) bool {
	return s != "" && !path.IsAbs(s) && path.Clean(s) == s && s != ".." && !strings.HasPrefix(s, "../") && !strings.ContainsAny(s, "\\\x00:\r\n")
}
func validLink(name, target string) bool {
	return target != "" && !path.IsAbs(target) && !strings.ContainsAny(target, "\\\x00:\r\n") && validName(path.Join(path.Dir(name), target))
}

// Archive includes dotfiles, WALs, session history, task definitions and all
// registered files, not just an agent's configuration directory. Device nodes,
// sockets and unsupported metadata are rejected rather than silently omitted.
func Archive(ctx context.Context, root *os.Root, dst io.Writer, limits Limits) (Report, error) {
	return ArchiveWithLayout(ctx, root, dst, limits, "")
}

// ArchiveWithLayout authorizes only the exact named release layout. Symlinks
// are archived as text; Wine drives and DeepSeek image packages are not read.
func ArchiveWithLayout(ctx context.Context, root *os.Root, dst io.Writer, limits Limits, layout Layout) (Report, error) {
	var report Report
	if root == nil || limits.MaxFiles <= 0 || limits.MaxBytes <= 0 || !layout.executable() {
		return report, ErrArchive
	}
	if e := layout.source(root); e != nil {
		return report, e
	}
	tw := tar.NewWriter(dst)
	err := fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return ErrArchive
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		if !layout.name(name) {
			return ErrArchive
		}
		info, e := root.Lstat(name)
		if e != nil {
			return ErrArchive
		}
		attrs, e := archiveMetadata(root, name, layout)
		if e != nil {
			return e
		}
		var link string
		if info.Mode()&os.ModeSymlink != 0 {
			link, e = root.Readlink(name)
			if e != nil {
				return ErrArchive
			}
		} else if !info.IsDir() && !info.Mode().IsRegular() {
			return ErrArchive
		}
		header, e := tar.FileInfoHeader(info, link)
		if e != nil {
			return ErrArchive
		}
		header.Name = name
		header.Format = tar.FormatPAX
		if len(attrs) != 0 {
			header.PAXRecords = attrs
		}
		if !layout.entry(header) {
			return ErrArchive
		}
		report.Files++
		if header.Size > limits.MaxBytes-report.Bytes {
			return ErrArchive
		}
		report.Bytes += header.Size
		if report.Files > limits.MaxFiles || report.Bytes > limits.MaxBytes {
			return ErrArchive
		}
		if e = tw.WriteHeader(header); e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, e := root.Open(name)
		if e != nil {
			return ErrArchive
		}
		defer file.Close()
		observed, e := file.Stat()
		if e != nil || !os.SameFile(info, observed) {
			return ErrArchive
		}
		n, e := io.CopyN(tw, &contextReader{ctx, file}, header.Size)
		if e != nil || n != header.Size {
			return ErrArchive
		}
		after, e := file.Stat()
		if e != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			return ErrArchive
		}
		return nil
	})
	if err != nil {
		return report, err
	}
	if err = tw.Close(); err != nil {
		return report, err
	}
	return report, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}

// Restore writes into an EMPTY, exclusively owned quarantine directory. The
// controller may publish it only after stream authentication, runtime config
// normalization and verification succeed. On failure it leaves the quarantine
// for owner-checked cleanup, never deletes arbitrary paths. Linux jobs must set
// preserveOwner=true; false exists for unprivileged local fixture tests.
func Restore(ctx context.Context, root *os.Root, src io.Reader, limits Limits, preserveOwner bool) (Report, error) {
	return RestoreWithLayout(ctx, root, src, limits, preserveOwner, "")
}

// RestoreWithLayout creates the fixed Wine Z: mapping last, after every file
// write and metadata operation. Its meaning is the future container root, not
// permission for the backup process to read/write outside this os.Root.
func RestoreWithLayout(ctx context.Context, root *os.Root, src io.Reader, limits Limits, preserveOwner bool, layout Layout) (Report, error) {
	var report Report
	if root == nil || limits.MaxFiles <= 0 || limits.MaxBytes <= 0 || !layout.executable() {
		return report, ErrArchive
	}
	entries, e := fs.ReadDir(root.FS(), ".")
	if e != nil || len(entries) > 0 {
		return report, ErrArchive
	}
	tr := tar.NewReader(&contextReader{ctx, src})
	seen := map[string]bool{}
	var directories, links []*tar.Header
	var wineRootLink *tar.Header
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return report, ErrArchive
		}
		special := h.Mode & 07000
		sharedDirectory := h.Typeflag == tar.TypeDir && special == 02000 && preserveOwner && runtime.GOOS == "linux"
		if !layout.entry(h) || seen[h.Name] || h.Size < 0 || h.Uid < 0 || h.Gid < 0 || (special != 0 && !sharedDirectory) {
			return report, ErrArchive
		}
		seen[h.Name] = true
		if !layout.pax(h) {
			return report, ErrArchive
		}
		report.Files++
		if h.Size > limits.MaxBytes-report.Bytes {
			return report, ErrArchive
		}
		report.Bytes += h.Size
		if report.Files > limits.MaxFiles || report.Bytes > limits.MaxBytes {
			return report, ErrArchive
		}
		if h.Name == "." && h.Typeflag != tar.TypeDir {
			return report, ErrArchive
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA && h.Size != 0 {
			return report, ErrArchive
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if h.Name != "." {
				if root.MkdirAll(h.Name, 0700) != nil {
					return report, ErrArchive
				}
			}
			directories = append(directories, h)
		case tar.TypeReg, tar.TypeRegA:
			if root.MkdirAll(path.Dir(h.Name), 0700) != nil {
				return report, ErrArchive
			}
			f, e := root.OpenFile(h.Name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				return report, ErrArchive
			}
			_, e = io.CopyN(f, tr, h.Size)
			closeErr := f.Close()
			if e != nil || closeErr != nil {
				return report, ErrArchive
			}
			if e = metadata(root, h, preserveOwner, layout); e != nil {
				return report, e
			}
		case tar.TypeSymlink:
			if layout.wine() && h.Name == layout.winePath(wineZ) {
				wineRootLink = h
				continue
			}
			links = append(links, h)
		default:
			return report, ErrArchive
		}
	}
	present, e := layout.wineData(root)
	if e != nil || (layout.wine() && (present != (seen[layout.winePath(wineC)] && seen[layout.winePath(wineZ)]) || (seen[layout.winePath(wineC)] != seen[layout.winePath(wineZ)]))) {
		return report, ErrArchive
	}
	if e = layout.deepSeekData(root); e != nil {
		return report, e
	}
	// No subsequent file write can traverse an archive-provided link.
	for _, h := range links {
		if root.MkdirAll(path.Dir(h.Name), 0700) != nil || root.Symlink(h.Linkname, h.Name) != nil {
			return report, ErrArchive
		}
		if preserveOwner && root.Lchown(h.Name, h.Uid, h.Gid) != nil {
			return report, ErrArchive
		}
	}
	sort.Slice(directories, func(i, j int) bool { return len(directories[i].Name) > len(directories[j].Name) })
	for _, h := range directories {
		if e = metadata(root, h, preserveOwner, layout); e != nil {
			return report, e
		}
	}
	if wineRootLink != nil {
		// Do not create parents here: all prefix directories were verified above.
		if root.Symlink("/", wineRootLink.Name) != nil {
			return report, ErrArchive
		}
		if preserveOwner && root.Lchown(wineRootLink.Name, wineRootLink.Uid, wineRootLink.Gid) != nil {
			return report, ErrArchive
		}
		// Link creation updates its parent mtime; reset only that verified real
		// directory, never follow the newly created root mapping.
		for _, h := range directories {
			if h.Name == layout.winePath(WinePrefix+"/dosdevices") && root.Chtimes(h.Name, h.ModTime, h.ModTime) != nil {
				return report, ErrArchive
			}
		}
	}
	return report, nil
}
func metadata(root *os.Root, h *tar.Header, owner bool, layout Layout) error {
	if owner && root.Chown(h.Name, h.Uid, h.Gid) != nil {
		return ErrArchive
	}
	mode := h.FileInfo().Mode().Perm()
	if h.Typeflag == tar.TypeDir && h.Mode&02000 != 0 {
		mode |= os.ModeSetgid
	}
	if root.Chmod(h.Name, mode) != nil {
		return ErrArchive
	}
	if root.Chtimes(h.Name, h.ModTime, h.ModTime) != nil {
		return ErrArchive
	}
	return restoreMetadata(root, h, layout)
}

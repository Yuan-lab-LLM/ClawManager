package systembackup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type FileRoot struct {
	ID   string `json:"id"`
	Path string `json:"-"`
}
type FileRecord struct {
	MetadataOnly bool      `json:"metadata_only,omitempty"`
	Path         string    `json:"path"`
	Size         int64     `json:"size"`
	Mode         int64     `json:"mode"`
	Type         byte      `json:"type"`
	Link         string    `json:"link,omitempty"`
	SHA256       string    `json:"sha256,omitempty"`
	UID          int       `json:"uid"`
	GID          int       `json:"gid"`
	ModifiedAt   time.Time `json:"modified_at"`
}
type FileBudget struct {
	MaxBytes   int64
	MaxEntries int
}

func validArchiveFileMode(kind byte, mode int64) bool {
	allowed := int64(0777)
	if kind == tar.TypeDir {
		allowed |= 02000
	} // Group inheritance is normal for Team shared directories.
	return mode >= 0 && mode & ^allowed == 0
}
func restoredArchiveMode(kind byte, mode int64) fs.FileMode {
	out := fs.FileMode(mode & 0777)
	if kind == tar.TypeDir && mode&02000 != 0 {
		out |= fs.ModeSetgid
	}
	return out
}

func (b FileBudget) validate() error {
	if b.MaxBytes <= 0 || b.MaxEntries <= 0 {
		return errors.New("file budget required")
	}
	return nil
}

// CaptureFiles preserves hidden/private/shared files. Roots must come from the
// server inventory, never request-provided paths. Cross-root symlinks become
// relative links inside the restored archive's roots directory.
func CaptureFiles(ctx context.Context, w io.Writer, roots []FileRoot, budget FileBudget) ([]FileRecord, error) {
	return captureFiles(ctx, w, roots, budget, nil)
}

func captureFiles(ctx context.Context, w io.Writer, roots []FileRoot, budget FileBudget, aliases map[string]string) ([]FileRecord, error) {
	return captureFilesWithPolicy(ctx, w, roots, budget, aliases, false)
}

func runtimeImageDependencyLink(rootID, relative, target string) bool {
	relative = filepath.ToSlash(relative)
	if !strings.HasPrefix(rootID, "instance_") || (!strings.HasPrefix(relative, "home/.openclaw/npm/") && !strings.HasPrefix(relative, "home/.openclaw/plugin-skills/")) {
		return false
	}
	target = path.Clean(filepath.ToSlash(target))
	return strings.HasPrefix(target, "/defaults/.openclaw/npm/projects/") || target == "/usr/local/lib/node_modules/openclaw" || strings.HasPrefix(target, "/usr/local/lib/node_modules/openclaw/")
}
func captureFilesWithPolicy(ctx context.Context, w io.Writer, roots []FileRoot, budget FileBudget, aliases map[string]string, imageDependencyMetadata bool) ([]FileRecord, error) {
	normalizedAliases := map[string]string{}
	for logical, physical := range aliases {
		if canonical, e := filepath.EvalSymlinks(physical); e == nil {
			physical = canonical
		}
		normalizedAliases[filepath.Clean(logical)] = physical
		if canonical, e := filepath.EvalSymlinks(logical); e == nil {
			normalizedAliases[canonical] = physical
		}
	}
	aliases = normalizedAliases
	if err := budget.validate(); err != nil {
		return nil, err
	}
	roots = append([]FileRoot(nil), roots...)
	sort.Slice(roots, func(i, j int) bool { return roots[i].ID < roots[j].ID })
	seen := map[string]bool{}
	for i := range roots {
		if !identifierPattern.MatchString(roots[i].ID) || seen[roots[i].ID] {
			return nil, errors.New("invalid or duplicate file root")
		}
		seen[roots[i].ID] = true
		p, err := filepath.EvalSymlinks(roots[i].Path)
		if err != nil {
			return nil, errors.New("file root unavailable")
		}
		p, err = filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		roots[i].Path = p
		for j := 0; j < i; j++ {
			if within(roots[j].Path, p) || within(p, roots[j].Path) {
				return nil, errors.New("overlapping file roots")
			}
		}
	}
	tw := tar.NewWriter(w)
	var records []FileRecord
	var total int64
	for _, source := range roots {
		root, err := os.OpenRoot(source.Path)
		if err != nil {
			return nil, err
		}
		err = fs.WalkDir(root.FS(), ".", func(rel string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(records) >= budget.MaxEntries {
				return errors.New("file entry budget exceeded")
			}
			info, err := root.Lstat(rel)
			if err != nil {
				return err
			}
			name := path.Join("roots", source.ID, filepath.ToSlash(rel))
			if !safeArchiveName(name) {
				return errors.New("invalid archive path")
			}
			link := ""
			var linkDescriptor []byte
			if info.Mode()&os.ModeSymlink != 0 {
				original, err := root.Readlink(rel)
				if err != nil {
					return err
				}
				target := original
				if !filepath.IsAbs(target) {
					target = filepath.Join(source.Path, filepath.FromSlash(path.Dir(rel)), target)
				}
				target = filepath.Clean(target)
				if filepath.IsAbs(original) {
					for logical, physical := range aliases {
						if within(logical, target) {
							rel, e := filepath.Rel(logical, target)
							if e != nil {
								return e
							}
							target = filepath.Join(physical, rel)
							break
						}
					}
				}
				mapped := ""
				for _, candidate := range roots {
					if within(candidate.Path, target) {
						relative, e := filepath.Rel(candidate.Path, target)
						if e != nil {
							return e
						}
						mapped = path.Join("roots", candidate.ID, filepath.ToSlash(relative))
						break
					}
				}
				if mapped == "" {
					if !imageDependencyMetadata || !runtimeImageDependencyLink(source.ID, rel, original) {
						return errors.New("symlink escapes managed file roots")
					}
					linkDescriptor, err = json.Marshal(map[string]any{"schema": "clawmanager.image-dependency-link.v1", "original_link": original, "original_mode": uint32(info.Mode()), "modified_at": info.ModTime().UTC()})
					if err != nil {
						return err
					}
				} else {
					link, err = filepath.Rel(filepath.FromSlash(path.Dir(name)), filepath.FromSlash(mapped))
					if err != nil {
						return err
					}
					link = filepath.ToSlash(link)
				}
			} else if !info.IsDir() && !info.Mode().IsRegular() {
				return errors.New("special file requires explicit capture policy")
			}
			header, err := tar.FileInfoHeader(info, link)
			if err != nil {
				return err
			}
			header.Name = name
			header.Format = tar.FormatPAX
			header.Uname = ""
			header.Gname = ""
			if linkDescriptor != nil {
				// The image is not mounted in this data-only target. Preserve the link
				// description as inert bytes, never follow or activate an external path.
				header.Typeflag = tar.TypeReg
				header.Linkname = ""
				header.Size = int64(len(linkDescriptor))
				header.Mode = 0600
			}
			if header.Size > budget.MaxBytes-total {
				return errors.New("file byte budget exceeded")
			}
			if err = tw.WriteHeader(header); err != nil {
				return err
			}
			record := FileRecord{Path: name, Size: header.Size, Mode: header.Mode, Type: header.Typeflag, Link: link, UID: header.Uid, GID: header.Gid, ModifiedAt: header.ModTime.UTC()}
			if linkDescriptor != nil {
				if e := writeFull(tw, linkDescriptor); e != nil {
					return e
				}
				sum := sha256.Sum256(linkDescriptor)
				record.SHA256 = hex.EncodeToString(sum[:])
				record.MetadataOnly = true
				total += int64(len(linkDescriptor))
			} else if info.Mode().IsRegular() {
				f, err := root.Open(rel)
				if err != nil {
					return err
				}
				opened, e := f.Stat()
				if e != nil || !os.SameFile(info, opened) {
					f.Close()
					return errors.New("file identity changed during capture")
				}
				h := sha256.New()
				n, e := io.Copy(io.MultiWriter(tw, h), contextReader{ctx, f})
				after, statErr := f.Stat()
				closeErr := f.Close()
				if e != nil {
					return e
				}
				if statErr != nil {
					return statErr
				}
				if closeErr != nil {
					return closeErr
				}
				if n != info.Size() || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
					return errors.New("file changed during capture")
				}
				record.SHA256 = hex.EncodeToString(h.Sum(nil))
				total += n
			}
			records = append(records, record)
			return nil
		})
		root.Close()
		if err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return records, nil
}

func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}
func safeArchiveName(name string) bool {
	return name != "" && !strings.Contains(name, "\\") && !strings.ContainsRune(name, 0) && !strings.Contains(name, ":") && path.Clean(name) == name && strings.HasPrefix(name, "roots/") && filepath.IsLocal(filepath.FromSlash(name))
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// ExtractFiles only accepts a newly-created, empty isolated directory supplied
// by the restore resource inventory. Symlinks are installed last and never
// followed while writing. Existing files are never overwritten.
func ExtractFiles(ctx context.Context, r io.Reader, target string, budget FileBudget) ([]FileRecord, error) {
	if err := budget.validate(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	if len(entries) != 0 {
		return nil, errors.New("restore file target must be empty")
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	tr := tar.NewReader(contextReader{ctx, r})
	seen := map[string]bool{}
	var records, links []FileRecord
	var total int64
	for {
		h, e := tr.Next()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, e
		}
		if !safeArchiveName(h.Name) || seen[h.Name] {
			return nil, errors.New("unsafe or duplicate archive path")
		}
		seen[h.Name] = true
		if len(records) >= budget.MaxEntries || h.Size < 0 || h.Size > budget.MaxBytes-total {
			return nil, errors.New("file restore budget exceeded")
		}
		record := FileRecord{Path: h.Name, Size: h.Size, Mode: h.Mode, Type: h.Typeflag, Link: h.Linkname, UID: h.Uid, GID: h.Gid, ModifiedAt: h.ModTime.UTC()}
		if h.Uid < 0 || h.Gid < 0 {
			return nil, errors.New("invalid file owner")
		}
		if !validArchiveFileMode(h.Typeflag, h.Mode) {
			return nil, errors.New("privileged file mode is not permitted")
		}
		if e = root.MkdirAll(path.Dir(h.Name), 0700); e != nil {
			return nil, e
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if h.Size != 0 {
				return nil, errors.New("directory has payload")
			}
			if e = root.MkdirAll(h.Name, 0700); e != nil {
				return nil, e
			}
		case tar.TypeReg:
			f, e := root.OpenFile(h.Name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return nil, e
			}
			hash := sha256.New()
			n, copyErr := io.Copy(io.MultiWriter(f, hash), tr)
			syncErr := f.Sync()
			closeErr := f.Close()
			if copyErr != nil {
				return nil, copyErr
			}
			if syncErr != nil {
				return nil, syncErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			if n != h.Size {
				return nil, io.ErrUnexpectedEOF
			}
			record.SHA256 = hex.EncodeToString(hash.Sum(nil))
			total += n
			if e = root.Chmod(h.Name, fs.FileMode(h.Mode)); e != nil {
				return nil, e
			}
		case tar.TypeSymlink:
			if h.Size != 0 || strings.ContainsAny(h.Linkname, "\\:\x00") || path.IsAbs(h.Linkname) || !safeArchiveName(path.Join(path.Dir(h.Name), h.Linkname)) {
				return nil, errors.New("symlink escapes restored roots")
			}
			links = append(links, record)
		default:
			return nil, fmt.Errorf("unsupported file archive entry type %d", h.Typeflag)
		}
		records = append(records, record)
	}
	// Our archive format has exactly the authenticated tar EOF, with no second
	// archive or ignored trailing payload after it.
	var extra [1]byte
	if n, e := io.ReadFull(contextReader{ctx, r}, extra[:]); n != 0 || e != io.EOF {
		return nil, errors.New("trailing file archive payload")
	}
	for _, link := range links {
		if err = root.Symlink(filepath.FromSlash(link.Link), filepath.FromSlash(link.Path)); err != nil {
			return nil, err
		}
	}
	// Apply directory permissions only after children are populated.
	for i := len(records) - 1; i >= 0; i-- {
		if err = restoreFileOwner(root, records[i]); err != nil {
			return nil, err
		}
		if records[i].Type != tar.TypeSymlink {
			if err = root.Chtimes(records[i].Path, records[i].ModifiedAt, records[i].ModifiedAt); err != nil {
				return nil, err
			}
		}
		if records[i].Type == tar.TypeDir {
			if err = root.Chmod(records[i].Path, restoredArchiveMode(records[i].Type, records[i].Mode)); err != nil {
				return nil, err
			}
		}
	}
	return records, nil
}

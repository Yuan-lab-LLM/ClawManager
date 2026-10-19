package services

import (
	"context"
	"encoding/json"
	"strings"
)

func (s *runtimeWorkspaceFileService) ReadText(ctx context.Context, scope WorkspaceFileScope, relative string) (*WorkspaceText, error) {
	return s.textOperation(ctx, scope, relative, "", "", false)
}
func (s *runtimeWorkspaceFileService) SaveText(ctx context.Context, scope WorkspaceFileScope, relative, text, version string) (*WorkspaceText, error) {
	return s.textOperation(ctx, scope, relative, text, version, true)
}
func (s *runtimeWorkspaceFileService) textOperation(ctx context.Context, scope WorkspaceFileScope, relative, text, version string, save bool) (*WorkspaceText, error) {
	rel, err := cleanWorkspaceRelativePath(relative)
	if err != nil {
		return nil, err
	}
	if !workspaceTextName(rel) || !validWorkspaceText(text) {
		return nil, ErrWorkspaceTextUnsupported
	}
	if len(text) > int(textPreviewMaxBytes) {
		return nil, ErrWorkspacePreviewTooLarge
	}
	if save && version == "" {
		return nil, ErrWorkspaceTextConflict
	}
	payload, _ := json.Marshal(map[string]any{"path": rel, "text": text, "version": version, "save": save})
	if save {
		if err := s.recordAudit(ctx, scope, "edit_attempt", rel, int64(len(text))); err != nil {
			return nil, err
		}
	}
	var result WorkspaceText
	err = s.runJSON(ctx, scope, []string{"python3", "-c", runtimeWorkspacePythonPrelude + runtimeWorkspaceTextScript, runtimeWorkspaceBase(scope)}, strings.NewReader(string(payload)), &result)
	return &result, err
}

const runtimeWorkspaceTextScript = `
import hashlib, fcntl
LIMIT = 1024 * 1024
req = json.loads(sys.stdin.buffer.read(8 * LIMIT))
rel = clean_rel(req['path'])
parts = rel.split('/')
parent = os.open(safe_base(sys.argv[1]), os.O_RDONLY | os.O_DIRECTORY)
try:
    for part in parts[:-1]:
        nxt = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
        os.close(parent)
        parent = nxt
    name = parts[-1]
    def read_doc():
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=parent)
        try:
            st = os.fstat(fd)
            if not stat.S_ISREG(st.st_mode) or st.st_nlink != 1:
                fail('text_unsupported')
            if st.st_size > LIMIT:
                fail('preview_too_large')
            with os.fdopen(os.dup(fd), 'rb') as stream:
                data = stream.read(LIMIT + 1)
            after = os.fstat(fd)
            if len(data)>LIMIT: fail('preview_too_large')
            if st.st_mtime_ns != after.st_mtime_ns or st.st_size != after.st_size: fail('text_conflict')
            try: text = data.decode('utf-8')
            except UnicodeDecodeError: fail('text_unsupported')
            if '\x00' in text: fail('text_unsupported')
            version = hashlib.sha256(str(st.st_mtime_ns).encode()+b':'+str(st.st_mode).encode()+b':'+data).hexdigest()
            return {'text':text,'version':version}, st
        finally: os.close(fd)
    lock = None
    if req['save']:
        lock = os.open('.clawmanager-editor-lock', os.O_RDWR|os.O_CREAT|os.O_NOFOLLOW, 0o600, dir_fd=parent)
        ls = os.fstat(lock)
        if not stat.S_ISREG(ls.st_mode) or ls.st_nlink!=1: fail('text_unsupported')
        fcntl.flock(lock, fcntl.LOCK_EX)
    doc, st = read_doc()
    if req['save']:
        if doc['version'] != req['version']: fail('text_conflict')
        data = req['text'].encode('utf-8')
        if len(data)>LIMIT: fail('preview_too_large')
        if b'\x00' in data: fail('text_unsupported')
        tmp = '.clawmanager-edit-' + os.urandom(16).hex()
        fd = os.open(tmp, os.O_WRONLY|os.O_CREAT|os.O_EXCL, 0o600, dir_fd=parent)
        try:
            os.fchown(fd, st.st_uid, st.st_gid)
            os.fchmod(fd, stat.S_IMODE(st.st_mode))
            with os.fdopen(fd,'wb') as stream:
                stream.write(data)
                stream.flush()
                os.fsync(stream.fileno())
            current, _ = read_doc()
            if current['version'] != req['version']: fail('text_conflict')
            os.replace(tmp,name,src_dir_fd=parent,dst_dir_fd=parent)
        finally:
            try: os.unlink(tmp,dir_fd=parent)
            except FileNotFoundError: pass
        doc, _ = read_doc()
    print(json.dumps(doc))
except FileNotFoundError:
    fail('not_found')
except (OSError, ValueError):
    fail('text_unsupported')
finally:
    os.close(parent)
`

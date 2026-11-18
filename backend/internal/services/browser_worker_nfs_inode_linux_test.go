//go:build linux

package services

import (
	"clawreef/internal/models"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNFSBrowserPrivateLeafRejectsForeignInodes(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("credential helper requires root")
	}
	for _, where := range []string{"source", "destination"} {
		for _, kind := range []string{"foreign-hardlink", "foreign-single", "foreign-directory", "foreign-symlink", "foreign-writable", "own"} {
			t.Run(where+"/"+kind, func(t *testing.T) {
				base, err := os.MkdirTemp("/tmp", "cm-browser-inodes-")
				if err != nil {
					t.Fatal(err)
				}
				defer os.RemoveAll(base)
				if err = os.Chown(base, 10001, 10001); err != nil {
					t.Fatal(err)
				}
				os.Chmod(base, 0711)
				script := `import os,sys
base,where,kind=sys.argv[1:]
def directory(p,uid,gid,mode):
 os.makedirs(p,exist_ok=True);os.chown(p,uid,gid);os.chmod(p,mode)
for p in ["openclaw","openclaw/user-3"]:directory(base+"/"+p,10001,10001,0o711)
p=base+"/openclaw/user-3/instance-12"
for tail in ["","/home","/home/.openclaw","/home/.openclaw/browser-worker"]:directory(p+tail,200012,200012,0o700)
if where=="source":leaf=p+"/home/.openclaw/browser-worker/chromium"
else:
 for tail in ["browsers","browsers/user-3"]:directory(base+"/"+tail,10001,10001,0o711)
 directory(base+"/browsers/user-3/instance-12",10001,200012,0o771)
 leaf=base+"/browsers/user-3/instance-12/profile"
directory(leaf,200012,200012,0o700);directory(leaf+"/nested",200012,200012,0o700)
f=leaf+"/nested/private";open(f,"w").write("own-private");os.chown(f,200012,200012);os.chmod(f,0o600);os.link(f,leaf+"/nested/own-link")
os.symlink("/tmp/browser-singleton",leaf+"/SingletonSocket");os.lchown(leaf+"/SingletonSocket",200012,200012)
bad=leaf+"/nested/foreign"
if kind=="foreign-hardlink":
 directory(base+"/outside",200013,200013,0o700);original=base+"/outside/data";open(original,"w").write("foreign-private");os.chown(original,200013,200013);os.chmod(original,0o644);os.link(original,bad)
elif kind=="foreign-single":open(bad,"w").write("foreign-private");os.chown(bad,200013,200013);os.chmod(bad,0o644)
elif kind=="foreign-directory":directory(bad,200013,200013,0o755)
elif kind=="foreign-symlink":os.symlink("/tmp/foreign",bad);os.lchown(bad,200013,200013)
elif kind=="foreign-writable":open(bad,"w").write("owned-but-shared");os.chown(bad,200012,200013);os.chmod(bad,0o660)
`
				out, err := exec.Command("python3", "-c", script, base, where, kind).CombinedOutput()
				if err != nil {
					t.Fatalf("fixture: %v %s", err, out)
				}
				workspace := RuntimeWorkspacePathWithRoot(base, RuntimeTypeOpenClaw, 3, 12)
				instance := &models.Instance{ID: 12, UserID: 3, RuntimeGeneration: 1, WorkspacePath: &workspace}
				ended, err := PrepareNFSBrowserProfile(context.Background(), base, instance)
				if !ended {
					t.Fatal("inode audit helper did not finish")
				}
				if kind != "own" {
					if err == nil {
						t.Fatal("foreign browser inode accepted")
					}
					return
				}
				if err != nil {
					t.Fatal("same-owner private data/hardlink/symlink rejected", err)
				}
				a, e := os.Stat(filepath.Join(base, "browsers/user-3/instance-12/profile/nested/private"))
				if e != nil {
					t.Fatal(e)
				}
				z, e := os.Stat(filepath.Join(base, "browsers/user-3/instance-12/profile/nested/own-link"))
				if e != nil || !os.SameFile(a, z) || a.Mode().Perm() != 0600 {
					t.Fatal("own private hardlink metadata changed", e)
				}
			})
		}
	}
}

func TestNFSBrowserPrivateLeafRealRootSquash(t *testing.T) {
	base := os.Getenv("CM_NFS_BROWSER_INODE_ROOT")
	if base == "" {
		t.Skip("isolated root-squash inode fixtures required")
	}
	for _, where := range []string{"source", "destination"} {
		for _, leaf := range []string{"profile", "downloads"} {
			for _, kind := range []string{"foreign-hardlink", "foreign-single", "own"} {
				t.Run(where+"/"+leaf+"/"+kind, func(t *testing.T) {
					root := filepath.Join(base, where+"-"+leaf+"-"+kind)
					workspace := RuntimeWorkspacePathWithRoot(root, RuntimeTypeOpenClaw, 3, 12)
					instance := &models.Instance{ID: 12, UserID: 3, RuntimeGeneration: 1, WorkspacePath: &workspace}
					ended, err := PrepareNFSBrowserProfile(t.Context(), root, instance)
					if !ended {
						t.Fatal("inode audit helper did not finish")
					}
					if kind != "own" {
						if err == nil {
							t.Fatal("foreign inode admitted on root-squash NFS")
						}
					} else if err != nil {
						t.Fatal("own hardlink/private data rejected on NFS", err)
					}
					inspect := `import os,sys
root,where,leaf,kind=sys.argv[1:]
old="chromium" if leaf=="profile" else "Downloads"
p=root+("/openclaw/user-3/instance-12/home/.openclaw/browser-worker/"+old if where=="source" and kind!="own" else "/browsers/user-3/instance-12/"+leaf)
f=p+"/nested/private";st=os.stat(f)
assert st.st_uid==200012 and st.st_gid==200012 and st.st_mode&0o777==0o600 and st.st_nlink==2
assert os.stat(p+"/nested/own-link").st_ino==st.st_ino and open(f).read()=="own-private"
if kind!="own":
 st=os.stat(p+"/nested/foreign");assert st.st_uid==200013 and st.st_mode&0o777==0o644
 assert st.st_nlink==(2 if kind=="foreign-hardlink" else 1)
 if where=="source":assert not os.path.exists(root+"/browsers/user-3/instance-12/"+leaf)
`
					out, err := exec.Command("setpriv", "--reuid=200012", "--regid=200012", "--clear-groups", "--", "python3", "-c", inspect, root, where, leaf, kind).CombinedOutput()
					if err != nil {
						t.Fatalf("read-only NFS audit/metadata check: %v %s", err, out)
					}
				})
			}
		}
	}
}

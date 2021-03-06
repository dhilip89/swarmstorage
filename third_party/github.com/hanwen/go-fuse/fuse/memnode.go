package fuse

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

var _ = log.Println

type MemNodeFs struct {
	DefaultNodeFileSystem
	backingStorePrefix string
	root               *memNode

	mutex    sync.Mutex
	nextFree int
}

func (fs *MemNodeFs) String() string {
	return fmt.Sprintf("MemNodeFs(%s)", fs.backingStorePrefix)
}

func (fs *MemNodeFs) Root() FsNode {
	return fs.root
}

func (fs *MemNodeFs) newNode() *memNode {
	fs.mutex.Lock()
	n := &memNode{
		fs: fs,
		id: fs.nextFree,
	}
	now := time.Now().UnixNano()
	n.info.SetNs(now, now, now)
	n.info.Mode = S_IFDIR | 0777
	fs.nextFree++
	fs.mutex.Unlock()
	return n
}

func NewMemNodeFs(prefix string) *MemNodeFs {
	me := &MemNodeFs{}
	me.backingStorePrefix = prefix
	me.root = me.newNode()
	return me
}

func (fs *MemNodeFs) Filename(n *Inode) string {
	mn := n.FsNode().(*memNode)
	return mn.filename()
}

type memNode struct {
	DefaultFsNode
	fs *MemNodeFs
	id int

	link string
	info Attr
}

func (n *memNode) newNode(isdir bool) *memNode {
	newNode := n.fs.newNode()
	n.Inode().New(isdir, newNode)
	return newNode
}

func (n *memNode) filename() string {
	return fmt.Sprintf("%s%d", n.fs.backingStorePrefix, n.id)
}

func (n *memNode) Deletable() bool {
	return false
}

func (n *memNode) Readlink(c *Context) ([]byte, Status) {
	return []byte(n.link), OK
}

func (n *memNode) Mkdir(name string, mode uint32, context *Context) (newNode FsNode, code Status) {
	ch := n.newNode(true)
	ch.info.Mode = mode | S_IFDIR
	n.Inode().AddChild(name, ch.Inode())
	return ch, OK
}

func (n *memNode) Unlink(name string, context *Context) (code Status) {
	ch := n.Inode().RmChild(name)
	if ch == nil {
		return ENOENT
	}
	return OK
}

func (n *memNode) Rmdir(name string, context *Context) (code Status) {
	return n.Unlink(name, context)
}

func (n *memNode) Symlink(name string, content string, context *Context) (newNode FsNode, code Status) {
	ch := n.newNode(false)
	ch.info.Mode = S_IFLNK | 0777
	ch.link = content
	n.Inode().AddChild(name, ch.Inode())

	return ch, OK
}

func (n *memNode) Rename(oldName string, newParent FsNode, newName string, context *Context) (code Status) {
	ch := n.Inode().RmChild(oldName)
	newParent.Inode().RmChild(newName)
	newParent.Inode().AddChild(newName, ch)
	return OK
}

func (n *memNode) Link(name string, existing FsNode, context *Context) (newNode FsNode, code Status) {
	n.Inode().AddChild(name, existing.Inode())
	return existing, code
}

func (n *memNode) Create(name string, flags uint32, mode uint32, context *Context) (file File, newNode FsNode, code Status) {
	ch := n.newNode(false)
	ch.info.Mode = mode | S_IFREG

	f, err := os.Create(ch.filename())
	if err != nil {
		return nil, nil, ToStatus(err)
	}
	n.Inode().AddChild(name, ch.Inode())
	return ch.newFile(f), ch, OK
}

type memNodeFile struct {
	LoopbackFile
	node *memNode
}

func (n *memNodeFile) String() string {
	return fmt.Sprintf("memNodeFile(%s)", n.LoopbackFile.String())
}

func (n *memNodeFile) InnerFile() File {
	return &n.LoopbackFile
}

func (n *memNodeFile) Flush() Status {
	code := n.LoopbackFile.Flush()
	var a Attr
	n.LoopbackFile.GetAttr(&a)
	n.node.info.Size = a.Size
	n.node.info.Blocks = a.Blocks
	return code
}

func (n *memNode) newFile(f *os.File) File {
	return &memNodeFile{
		LoopbackFile: LoopbackFile{File: f},
		node:         n,
	}
}

func (n *memNode) Open(flags uint32, context *Context) (file File, code Status) {
	f, err := os.OpenFile(n.filename(), int(flags), 0666)
	if err != nil {
		return nil, ToStatus(err)
	}

	return n.newFile(f), OK
}

func (n *memNode) GetAttr(fi *Attr, file File, context *Context) (code Status) {
	*fi = n.info
	return OK
}

func (n *memNode) Truncate(file File, size uint64, context *Context) (code Status) {
	if file != nil {
		code = file.Truncate(size)
	} else {
		err := os.Truncate(n.filename(), int64(size))
		code = ToStatus(err)
	}
	if code.Ok() {
		n.info.SetNs(-1, -1, time.Now().UnixNano())
		// TODO - should update mtime too?
		n.info.Size = size
	}
	return code
}

func (n *memNode) Utimens(file File, atime int64, mtime int64, context *Context) (code Status) {
	n.info.SetNs(int64(atime), int64(mtime), time.Now().UnixNano())
	return OK
}

func (n *memNode) Chmod(file File, perms uint32, context *Context) (code Status) {
	n.info.Mode = (n.info.Mode ^ 07777) | perms
	n.info.SetNs(-1, -1, time.Now().UnixNano())
	return OK
}

func (n *memNode) Chown(file File, uid uint32, gid uint32, context *Context) (code Status) {
	n.info.Uid = uid
	n.info.Gid = gid
	n.info.SetNs(-1, -1, time.Now().UnixNano())
	return OK
}

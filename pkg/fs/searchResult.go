// +build linux darwin

/*
Copyright 2013 The Perkeep Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package fs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"perkeep.org/pkg/blob"
	"perkeep.org/pkg/search"
)

type searchResultDir struct {
	fs *CamliFileSystem

	searchExp   string
	mu          sync.Mutex
	ents        map[string]*search.DescribedBlob // filename to blob meta
	modTime     map[string]time.Time             // filename to permanode modtime
	lastReaddir time.Time
	lastNames   []string
}

var (
	_ fs.Node               = (*searchResultDir)(nil)
	_ fs.HandleReadDirAller = (*searchResultDir)(nil)
	_ fs.NodeStringLookuper = (*searchResultDir)(nil)
)

func (n *searchResultDir) Attr(ctx context.Context, a *fuse.Attr) error {
	a.Mode = os.ModeDir | 0555
	a.Uid = uint32(os.Getuid())
	a.Gid = uint32(os.Getgid())
	return nil
}

func (n *searchResultDir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	var ents []fuse.Dirent

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.lastReaddir.After(time.Now().Add(-searchSearchInterval)) {
		Logger.Printf("fs.search: ReadDirAll from cache")
		for _, name := range n.lastNames {
			ents = append(ents, fuse.Dirent{Name: name})
		}
		return ents, nil
	}

	Logger.Printf("fs.search: ReadDirAll, doing search for '%s'", n.searchExp)

	n.ents = make(map[string]*search.DescribedBlob)
	n.modTime = make(map[string]time.Time)

	req := &search.SearchQuery{
		Expression: n.searchExp,
		Limit:      -1,
		Describe: &search.DescribeRequest{
			Rules: []*search.DescribeRule{
				{
					Attrs: []string{"camliContent", "camliContentImage", "camliMember"},
				},
			},
		},
	}
	res, err := n.fs.client.Query(ctx, req)
	if err != nil {
		Logger.Printf("fs.search: GetRecentPermanodes error in ReadDirAll: %v", err)
		return nil, fuse.EIO
	}

	n.lastNames = nil
	for _, ri := range res.Blobs {
		br := ri.Blob
		modTime := time.Now()
		if res.Describe == nil || res.Describe.Meta == nil {
			Logger.Printf("fs.search: res.Describe nil")
			continue
		}
		if res.Describe.Meta == nil {
			Logger.Printf("fs.search: Describe.Meta nil")
			continue
		}
		meta := res.Describe.Meta.Get(br)
		if meta == nil {
			Logger.Printf("fs.search: Meta for br is nil")
			continue
		}
		if meta.Permanode == nil {
			Logger.Printf("fs.search: br meta permanode is nil")
			continue
		}
		cc, ok := blob.Parse(meta.Permanode.Attr.Get("camliContent"))
		if !ok {
			continue
		}
		ccMeta := res.Describe.Meta.Get(cc)
		if ccMeta == nil {
			continue
		}
		var name string
		switch {
		case ccMeta.File != nil:
			name = ccMeta.File.FileName
			if mt := ccMeta.File.Time; !mt.IsAnyZero() {
				modTime = mt.Time()
			}
		case ccMeta.Dir != nil:
			name = ccMeta.Dir.FileName
		default:
			continue
		}
		if name == "" || n.ents[name] != nil {
			ext := filepath.Ext(name)
			if ext == "" && ccMeta.File != nil && strings.HasSuffix(ccMeta.File.MIMEType, "image/jpeg") {
				ext = ".jpg"
			}
			name = strings.TrimPrefix(ccMeta.BlobRef.String(), ccMeta.BlobRef.HashName()+"-")[:10] + ext
			if n.ents[name] != nil {
				continue
			}
		}
		n.ents[name] = ccMeta
		n.modTime[name] = modTime
		Logger.Printf("fs.search: name %q = %v (at %v)", name, ccMeta.BlobRef, modTime)
		n.lastNames = append(n.lastNames, name)
		ents = append(ents, fuse.Dirent{
			Name: name,
		})
	}
	Logger.Printf("fs.search returning %d entries", len(ents))
	n.lastReaddir = time.Now()
	return ents, nil
}

type searchResultFile struct {
	node
}

var _ fs.Node = (*searchResultFile)(nil)

func (n *searchResultFile) Attr(ctx context.Context, a *fuse.Attr) error {
	n.node.Attr(ctx, a)
	a.Mode = 0666
	a.Uid = uint32(os.Getuid())
	a.Gid = uint32(os.Getgid())
	return nil
}

func (n *searchResultDir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	Logger.Printf("fs.searchResultDir: Lookup(%q)", name)
	if n.ents == nil {
		// Odd case: a Lookup before a Readdir. Force a readdir to
		// seed our map. Mostly hit just during development.
		n.mu.Unlock() // release, since ReadDirAll will acquire
		n.ReadDirAll(ctx)
		n.mu.Lock()
	}
	db := n.ents[name]
	Logger.Printf("fs.searchResultDir: Lookup(%q) = %v", name, db)
	if db == nil {
		return nil, fuse.ENOENT
	}

	nod := &searchResultFile{
		node: node{
			fs:           n.fs,
			blobref:      db.BlobRef,
			pnodeModTime: n.modTime[name],
		},
	}
	blob, err := nod.fs.fetchSchemaMeta(ctx, nod.blobref)
	if err != nil {
		Logger.Printf("fs:searchResultDir: Couldn't find meta")
	} else {
		Logger.Printf("fs:searchResultDir: Blob type: %s", blob.Type())
	}

	return nod, nil
}

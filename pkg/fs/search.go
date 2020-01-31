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
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
)

// searchDir implements fuse.Node and is a directory of search
// permanodes' files, for permanodes with a camliContent pointing to a
// "file".
type searchDir struct {
	fs *CamliFileSystem
}

var (
	_ fs.Node               = (*searchDir)(nil)
	_ fs.HandleReadDirAller = (*searchDir)(nil)
	_ fs.NodeStringLookuper = (*searchDir)(nil)
)

func (n *searchDir) Attr(ctx context.Context, a *fuse.Attr) error {
	a.Mode = os.ModeDir | 0500
	a.Uid = uint32(os.Getuid())
	a.Gid = uint32(os.Getgid())
	return nil
}

const searchSearchInterval = 10 * time.Second

func (n *searchDir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	return []fuse.Dirent{
		{Name: "README.txt"},
	}, nil
}

const searchReadme = `
You are now in the "search" filesystem, where you can use 
Perkeep's search functionality from the FUSE mount.  

Usage: cd "<search query>", e.g.:

	cd "after:\"2015-10-01\" and is:image"

`

func (n *searchDir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	if name == "README.txt" {
		return staticFileNode(searchReadme), nil
	}
	return &searchResultDir{fs: n.fs, searchExp: name}, nil
}

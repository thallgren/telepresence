package ftp

import (
	"context"
	"io/fs"
	"path/filepath"

	"github.com/spf13/afero"

	"github.com/datawire/dlib/dlog"
)

func SymLinkResolvingFs(ctx context.Context, fs afero.Fs) afero.Fs {
	return &symlinkResolvingFs{Fs: fs, ctx: ctx}
}

// symlinkResolvingFs does not implement afero.Lstater, afero.Linker, or afero.LinkReader. It will
// always return a symLinkResolvingFile on Create, Open, and OpenFile
type symlinkResolvingFs struct {
	afero.Fs
	ctx context.Context
}

// symlinkResolvingFile will replace symlinks returned by Readdir() with the resolved result
type symlinkResolvingFile struct {
	afero.File
	fs *symlinkResolvingFs
}

func (h *symlinkResolvingFs) Create(name string) (afero.File, error) {
	f, err := h.Fs.Create(name)
	if err == nil {
		f = &symlinkResolvingFile{File: f, fs: h}
		dlog.Debugf(h.ctx, "fs.Create(%s) successful", f.Name())
	} else {
		dlog.Errorf(h.ctx, "fs.Create(%s) failed: %v", name, err)
	}
	return f, err
}

func (h *symlinkResolvingFs) Open(name string) (afero.File, error) {
	f, err := h.Fs.Open(name)
	if err == nil {
		f = &symlinkResolvingFile{File: f, fs: h}
		dlog.Debugf(h.ctx, "fs.Open(%s) successful", f.Name())
	} else {
		dlog.Errorf(h.ctx, "fs.Open(%s) failed: %v", name, err)
	}
	return f, err
}

func (h *symlinkResolvingFs) OpenFile(name string, flag int, perm fs.FileMode) (afero.File, error) {
	f, err := h.Fs.OpenFile(name, flag, perm)
	if err == nil {
		f = &symlinkResolvingFile{File: f, fs: h}
		dlog.Debugf(h.ctx, "fs.OpenFile(%s, %d, %s) successful", f.Name(), flag, perm)
	} else {
		dlog.Errorf(h.ctx, "fs.OpenFile(%s, %d, %s) failed: %v", name, flag, perm, err)
	}
	return f, err
}

func (h *symlinkResolvingFile) Close() error {
	err := h.File.Close()
	if err == nil {
		dlog.Debugf(h.fs.ctx, "file.Close(%s) successful", h.Name())
	} else {
		dlog.Errorf(h.fs.ctx, "file.Close(%s) failed: %s", h.Name(), err)
	}
	return err
}

func (h *symlinkResolvingFile) Read(buf []byte) (int, error) {
	n, err := h.File.Read(buf)
	if err == nil {
		dlog.Tracef(h.fs.ctx, "file.Read(%s) %d bytes", h.Name(), n)
	} else {
		dlog.Tracef(h.fs.ctx, "file.Read(%s) failed: %s", h.Name(), err)
	}
	return n, err
}

func (h *symlinkResolvingFile) Write(buf []byte) (int, error) {
	n, err := h.File.Write(buf)
	if err == nil {
		dlog.Tracef(h.fs.ctx, "file.Write(%s) %d bytes", h.Name(), n)
	} else {
		dlog.Tracef(h.fs.ctx, "file.Write(%s) failed: %s", h.Name(), err)
	}
	return n, err
}

func (h *symlinkResolvingFile) Readdir(count int) ([]fs.FileInfo, error) {
	fis, err := h.File.Readdir(count) //nolint:forbidigo // We actually reimplement this method
	if err != nil {
		return nil, err
	}
	for i, fi := range fis {
		if (fi.Mode() & fs.ModeSymlink) != 0 {
			// replace with resolved FileInfo from Stat()
			if fis[i], err = h.fs.Stat(filepath.Join(h.Name(), fi.Name())); err != nil {
				return nil, err
			}
		}
	}
	return fis, nil
}

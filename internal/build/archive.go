// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package build

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Archive interface {
	// Directory adds a new directory entry to the archive and sets the
	// directory for subsequent calls to Header.
	Directory(name string) error

	// Header adds a new file to the archive. The file is added to the directory
	// set by Directory. The content of the file must be written to the returned
	// writer.
	Header(os.FileInfo) (io.Writer, error)

	// Close flushes the archive and closes the underlying file.
	Close() error
}

func NewArchive(file *os.File) (Archive, string) {
	switch {
	case strings.HasSuffix(file.Name(), ".zip"):
		return NewZipArchive(file), strings.TrimSuffix(file.Name(), ".zip")
	case strings.HasSuffix(file.Name(), ".tar.gz"):
		return NewTarballArchive(file), strings.TrimSuffix(file.Name(), ".tar.gz")
	default:
		return nil, ""
	}
}

// AddFile appends an existing file to an archive.
func AddFile(a Archive, file string) error {
	fd, err := os.Open(file)
	if err != nil {
		return err
	}
	defer fd.Close()
	fi, err := fd.Stat()
	if err != nil {
		return err
	}
	w, err := a.Header(fi)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, fd); err != nil {
		return err
	}
	return nil
}

// WriteArchive creates an archive containing the given files.
func WriteArchive(name string, files []string) (err error) {
	archfd, err := os.Create(name)
	if err != nil {
		return err
	}

	defer func() {
		archfd.Close()
		// Remove the half-written archive on failure.
		if err != nil {
			os.Remove(name)
		}
	}()
	archive, basename := NewArchive(archfd)
	if archive == nil {
		return fmt.Errorf("unknown archive extension")
	}
	fmt.Println(name)
	if err := archive.Directory(basename); err != nil {
		return err
	}
	for _, file := range files {
		fmt.Println("   +", filepath.Base(file))
		if err := AddFile(archive, file); err != nil {
			return err
		}
	}
	return archive.Close()
}

type ZipArchive struct {
	dir  string
	zipw *zip.Writer
	file io.Closer
}

func NewZipArchive(w io.WriteCloser) Archive {
	return &ZipArchive{"", zip.NewWriter(w), w}
}

func (a *ZipArchive) Directory(name string) error {
	a.dir = name + "/"
	return nil
}

func (a *ZipArchive) Header(fi os.FileInfo) (io.Writer, error) {
	head, err := zip.FileInfoHeader(fi)
	if err != nil {
		return nil, fmt.Errorf("can't make zip header: %v", err)
	}
	head.Name = a.dir + head.Name
	head.Method = zip.Deflate
	w, err := a.zipw.CreateHeader(head)
	if err != nil {
		return nil, fmt.Errorf("can't add zip header: %v", err)
	}
	return w, nil
}

func (a *ZipArchive) Close() error {
	if err := a.zipw.Close(); err != nil {
		return err
	}
	return a.file.Close()
}

type TarballArchive struct {
	dir  string
	tarw *tar.Writer
	gzw  *gzip.Writer
	file io.Closer
}

func NewTarballArchive(w io.WriteCloser) Archive {
	gzw := gzip.NewWriter(w)
	tarw := tar.NewWriter(gzw)
	return &TarballArchive{"", tarw, gzw, w}
}

func (a *TarballArchive) Directory(name string) error {
	a.dir = name + "/"
	return a.tarw.WriteHeader(&tar.Header{
		Name:     a.dir,
		Mode:     0755,
		Typeflag: tar.TypeDir,
	})
}

func (a *TarballArchive) Header(fi os.FileInfo) (io.Writer, error) {
	head, err := tar.FileInfoHeader(fi, "")
	if err != nil {
		return nil, fmt.Errorf("can't make tar header: %v", err)
	}
	head.Name = a.dir + head.Name
	if err := a.tarw.WriteHeader(head); err != nil {
		return nil, fmt.Errorf("can't add tar header: %v", err)
	}
	return a.tarw, nil
}

func (a *TarballArchive) Close() error {
	if err := a.tarw.Close(); err != nil {
		return err
	}
	if err := a.gzw.Close(); err != nil {
		return err
	}
	return a.file.Close()
}

type ArchiveReader interface {
	// tar | zip
	Type() string

	// Without extension
	BareName() string

	// Read filenames in root directory of the archive
	TopFiles() []string

	// Close all associated streams
	Close() error
}

type ZipArchiveReader struct {
	zipr     *zip.ReadCloser
	bareName string
}

func NewZipArchiveReader(name string) (ArchiveReader, error) {
	zipr, err := zip.OpenReader(name)
	if err != nil {
		return nil, err
	}

	return &ZipArchiveReader{zipr, name[:len(name)-4]}, nil
}

func (a *ZipArchiveReader) Type() string { return "zip" }

func (a *ZipArchiveReader) BareName() string { return a.bareName }

func (a *ZipArchiveReader) TopFiles() []string {
	filenames := make([]string, 2)
	for _, file := range a.zipr.File {
		if !file.FileInfo().IsDir() {
			filenames = append(filenames, file.Name)
		}
	}
	return filenames
}

func (a *ZipArchiveReader) Close() error {
	a.zipr.Close()
	return nil
}

type TarballArchiveReader struct {
	gzipr    *gzip.Reader
	tarr     *tar.Reader
	bareName string
}

func NewTarballArchiveReader(file *os.File) (ArchiveReader, error) {
	gzipr, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	tarr := tar.NewReader(gzipr)
	name := file.Name()
	return &TarballArchiveReader{gzipr, tarr, name[:len(name)-7]}, nil
}

func (a *TarballArchiveReader) Type() string { return "tar" }

func (a *TarballArchiveReader) BareName() string { return a.bareName }

func (a *TarballArchiveReader) TopFiles() (filenames []string) {
	for {
		header, err := a.tarr.Next()
		if err == io.EOF {
			return
		}
		if !header.FileInfo().IsDir() {
			filenames = append(filenames, header.Name)
		}
	}
}

func (a *TarballArchiveReader) Close() error {
	a.gzipr.Close()
	return nil
}

func OpenArchive(filename string, file *os.File) (ArchiveReader, error) {
	if strings.HasSuffix(filename, ".zip") {
		return NewZipArchiveReader(filename)
	}
	if strings.HasSuffix(filename, ".tar.gz") {
		return NewTarballArchiveReader(file)
	}
	return nil, fmt.Errorf("unsupported archive" + filename)
}

// InvestigateArchive looks into an existing archive.
func InvestigateArchive(filename string) (binaryNames [2]string, archiveType, md5String string, err error) {
	file, err := os.Open(filename)
	if err != nil {
		return
	}
	defer file.Close()

	archive, err := OpenArchive(filename, file)
	if archive == nil {
		return
	}

	archiveType = archive.Type()
	files := archive.TopFiles()
	for _, f := range files {
		delimIndex := strings.LastIndex(f, "/")
		if delimIndex > 0 && f[delimIndex+1:delimIndex+5] == "geth" {
			binaryNames[0] = f[delimIndex+1:]
			binaryNames[1] = f
			break
		}
	}

	archive.Close()

	hash := md5.New()
	if _, err = io.Copy(hash, file); err != nil {
		return
	}
	hashInBytes := hash.Sum(nil)[:16]
	md5String = hex.EncodeToString(hashInBytes)
	return
}

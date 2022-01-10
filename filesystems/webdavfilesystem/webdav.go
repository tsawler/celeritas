package webdavfilesystem

import (
	"fmt"
	"github.com/studio-b12/gowebdav"
	"github.com/tsawler/celeritas/filesystems"
	"io"
	"os"
	"path"
	"strings"
)

// WebDAV is the type for the webdav file system
type WebDAV struct {
	Host string
	User string
	Pass string
}

// getCredentials returns a webdav client using values from the receiver
func (w *WebDAV) getCredentials() *gowebdav.Client {
	c := gowebdav.NewClient(w.Host, w.User, w.Pass)
	return c
}

// Put attempts to put a file on the remote file system
func (w *WebDAV) Put(fileName, folder string) error {
	client := w.getCredentials()

	file, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	err = client.WriteStream(fmt.Sprintf("%s/%s", folder, path.Base(fileName)), file, 0644)
	if err != nil {
		return err
	}
	return nil
}

// List lists files on the remote file system
func (w *WebDAV) List(prefix string) ([]filesystems.Listing, error) {
	var listing []filesystems.Listing
	client := w.getCredentials()
	files, err := client.ReadDir(prefix)
	if err != nil {
		return listing, err
	}

	for _, file := range files {
		if !strings.HasPrefix(file.Name(), ".") {
			b := float64(file.Size())
			kb := b / 1024
			mb := kb / 1024
			current := filesystems.Listing{
				LastModified: file.ModTime(),
				Key: file.Name(),
				Size: mb,
				IsDir: file.IsDir(),
			}
			listing = append(listing, current)
		}
	}

	return listing, nil
}

// Delete deletes file(s) from the remote file system
func (w *WebDAV) Delete(itemsToDelete []string) bool {
	client := w.getCredentials()
	for _, item := range itemsToDelete {
		err := client.Remove(item)
		if err != nil {
			return false
		}
	}
	return true
}

// Get retrieves file(s) from the remote file system
func (w *WebDAV) Get(destination string, items ...string) error {
	client := w.getCredentials()

	for _, item := range items{
		err := func() error{
			webdavFilePath := item
			localFilePath := fmt.Sprintf("%s/%s", destination, path.Base(item))

			reader, err := client.ReadStream(webdavFilePath)
			if err != nil {
				return err
			}

			file, err := os.Create(localFilePath)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(file, reader)
			if err != nil {
				return err
			}

			return nil
		}()
		if err != nil {
			return err
		}
	}
	return nil
}

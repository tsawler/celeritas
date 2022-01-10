package celeritas

import (
	"errors"
	"fmt"
	"github.com/gabriel-vasile/mimetype"
	"github.com/tsawler/celeritas/filesystems"
	"io"
	"net/http"
	"os"
	"path"
)

func (c *Celeritas) UploadFile(r *http.Request, destination, field string, fs filesystems.FS) error {
	fileName, err := c.getFileToUpload(r, field)
	if err != nil {
		c.ErrorLog.Println(err)
		return err
	}

	if fs != nil {
		err = fs.Put(fileName, destination)
		if err != nil {
			c.ErrorLog.Println(err)
			return err
		}
	} else {
		err = os.Rename(fileName, fmt.Sprintf("%s/%s", destination, path.Base(fileName)))
		if err != nil {
			c.ErrorLog.Println(err)
			return err
		}
	}

	defer func() {
		_ = os.Remove(fileName)
	}()

	return nil
}

func (c *Celeritas) getFileToUpload(r *http.Request, fieldName string) (string, error) {
	_ = r.ParseMultipartForm(c.config.uploads.maxUploadSize)

	file, header, err := r.FormFile(fieldName)
	if err != nil {
		return "", err
	}
	defer file.Close()

	mimeType, err := mimetype.DetectReader(file)
	if err != nil {
		return "", err
	}

	// go back to start of file
	_, err = file.Seek(0, 0)
	if err != nil {
		return "", err
	}

	if !inSlice(c.config.uploads.allowedMimeTypes, mimeType.String()) {
		return "", errors.New("invalid file type uploaded")
	}

	dst, err := os.Create(fmt.Sprintf("./tmp/%s", header.Filename))
	if err != nil {
		return "", err
	}
	defer dst.Close()

	_, err = io.Copy(dst, file)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("./tmp/%s", header.Filename), nil
}

func inSlice(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
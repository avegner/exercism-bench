package main

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
)

// copyFile copies only a regular file.
func copyFile(srcPath, destPath string) error {
	// check file type
	fi, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	if !regular(fi) {
		return errors.New("not a regular file")
	}

	// copy data
	bs, err := ioutil.ReadFile(srcPath)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(destPath, bs, fi.Mode())
}

// copyFiles copies all files from srcDir to destDir.
// All nested dirs are ignored.
func copyFiles(srcDir, destDir string) error {
	fis, err := ioutil.ReadDir(srcDir)
	if err != nil {
		return err
	}

	for _, fi := range fis {
		if fi.IsDir() {
			continue
		}
		n := fi.Name()
		if err = copyFile(filepath.Join(srcDir, n), filepath.Join(destDir, n)); err != nil {
			return err
		}
	}
	return nil
}

func regular(fi os.FileInfo) bool {
	return fi.Mode()&os.ModeType == 0
}

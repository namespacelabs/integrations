package examples

import (
	"os"
	"path/filepath"
)

func ComputeBaseDir(basedir string) (string, error) {
	if basedir != "" {
		return basedir, nil
	}

	us, err := executablePath()
	if err != nil {
		return "", err
	}

	return filepath.Dir(us), nil
}

func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}

	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}

	return exe, nil
}

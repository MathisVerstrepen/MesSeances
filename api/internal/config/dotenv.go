package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// LoadDotEnv loads the nearest supported .env file without overriding the
// process environment. A missing file is not an error.
func LoadDotEnv() error {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return err
	}
	return loadDotEnvFrom(workingDirectory)
}

func loadDotEnvFrom(workingDirectory string) error {
	paths := []string{filepath.Join(workingDirectory, "deploy", ".env")}
	parent := filepath.Dir(workingDirectory)
	if parent != workingDirectory {
		paths = append(paths, filepath.Join(parent, "deploy", ".env"))
	}

	for _, path := range paths {
		err := godotenv.Load(path)
		if err == nil {
			return nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

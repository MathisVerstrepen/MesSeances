package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvFromMissingFiles(t *testing.T) {
	workingDirectory := filepath.Join(t.TempDir(), "api")
	if err := os.Mkdir(workingDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := loadDotEnvFrom(workingDirectory); err != nil {
		t.Fatalf("load missing dotenv: %v", err)
	}
}

func TestLoadDotEnvFromCurrentDirectory(t *testing.T) {
	workingDirectory := filepath.Join(t.TempDir(), "api")
	if err := os.Mkdir(workingDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	currentKey := "MESSEANCES_DOTENV_CURRENT_TEST"
	parentKey := "MESSEANCES_DOTENV_PARENT_IGNORED_TEST"
	unsetEnv(t, currentKey)
	unsetEnv(t, parentKey)
	writeDotEnv(t, filepath.Join(workingDirectory, ".env"), currentKey+"=current\n")
	writeDotEnv(t, filepath.Join(filepath.Dir(workingDirectory), ".env"), parentKey+"=parent\n")

	if err := loadDotEnvFrom(workingDirectory); err != nil {
		t.Fatalf("load current dotenv: %v", err)
	}
	if got := os.Getenv(currentKey); got != "current" {
		t.Fatalf("current value=%q", got)
	}
	if _, exists := os.LookupEnv(parentKey); exists {
		t.Fatal("parent dotenv was merged with current dotenv")
	}
}

func TestLoadDotEnvFromParentDirectory(t *testing.T) {
	workingDirectory := filepath.Join(t.TempDir(), "api")
	if err := os.Mkdir(workingDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	key := "MESSEANCES_DOTENV_PARENT_TEST"
	unsetEnv(t, key)
	writeDotEnv(t, filepath.Join(filepath.Dir(workingDirectory), ".env"), key+"=parent\n")

	if err := loadDotEnvFrom(workingDirectory); err != nil {
		t.Fatalf("load parent dotenv: %v", err)
	}
	if got := os.Getenv(key); got != "parent" {
		t.Fatalf("parent value=%q", got)
	}
}

func TestLoadDotEnvPreservesProcessEnvironment(t *testing.T) {
	workingDirectory := t.TempDir()
	key := "MESSEANCES_DOTENV_PREEXISTING_TEST"
	t.Setenv(key, "shell")
	writeDotEnv(t, filepath.Join(workingDirectory, ".env"), key+"=file\n")

	if err := loadDotEnvFrom(workingDirectory); err != nil {
		t.Fatalf("load dotenv: %v", err)
	}
	if got := os.Getenv(key); got != "shell" {
		t.Fatalf("process value overridden: %q", got)
	}
}

func TestLoadDotEnvRejectsMalformedExistingFile(t *testing.T) {
	workingDirectory := t.TempDir()
	writeDotEnv(t, filepath.Join(workingDirectory, ".env"), "VALID=value\nMALFORMED LINE\n")

	if err := loadDotEnvFrom(workingDirectory); err == nil {
		t.Fatal("expected malformed dotenv error")
	}
}

func TestLoadDotEnvRejectsUnreadableExistingPath(t *testing.T) {
	workingDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(workingDirectory, ".env"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := loadDotEnvFrom(workingDirectory); err == nil {
		t.Fatal("expected dotenv read error")
	}
}

func writeDotEnv(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	value, exists := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if exists {
			_ = os.Setenv(key, value)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

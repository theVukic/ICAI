package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const appDataFolderName = "ICAIapp"

type appStoragePaths struct {
	RootDir        string
	BackendDir     string
	DescriptionDir string
}

func initializeAppStorage(workDir string, projectDir string, exeDir string) (appStoragePaths, error) {
	rootDir, err := resolveAppStorageRoot()
	if err != nil {
		return appStoragePaths{}, err
	}

	paths := appStoragePaths{
		RootDir:        rootDir,
		BackendDir:     filepath.Join(rootDir, "backend"),
		DescriptionDir: filepath.Join(rootDir, "description"),
	}

	if err := os.MkdirAll(paths.BackendDir, 0o755); err != nil {
		return appStoragePaths{}, fmt.Errorf("nije moguće kreirati app backend storage: %w", err)
	}

	if err := bootstrapFileIfMissing(
		filepath.Join(paths.BackendDir, "token.json"),
		filepath.Join(exeDir, "backend", "token.json"),
		filepath.Join(projectDir, "backend", "token.json"),
		filepath.Join(workDir, "backend", "token.json"),
		filepath.Join(exeDir, "token.json"),
		filepath.Join(projectDir, "token.json"),
		filepath.Join(workDir, "token.json"),
	); err != nil {
		return appStoragePaths{}, err
	}

	if err := bootstrapFileIfMissing(
		filepath.Join(paths.BackendDir, "pepper.json"),
		filepath.Join(exeDir, "backend", "pepper.json"),
		filepath.Join(projectDir, "backend", "pepper.json"),
		filepath.Join(workDir, "backend", "pepper.json"),
		filepath.Join(exeDir, "pepper.json"),
		filepath.Join(projectDir, "pepper.json"),
		filepath.Join(workDir, "pepper.json"),
	); err != nil {
		return appStoragePaths{}, err
	}

	if err := bootstrapFileIfMissing(
		filepath.Join(paths.RootDir, "session.json"),
		filepath.Join(exeDir, "session.json"),
		filepath.Join(projectDir, "session.json"),
		filepath.Join(workDir, "session.json"),
		filepath.Join(exeDir, "backend", "session.json"),
		filepath.Join(projectDir, "backend", "session.json"),
		filepath.Join(workDir, "backend", "session.json"),
	); err != nil {
		return appStoragePaths{}, err
	}

	if err := bootstrapFileIfMissing(
		filepath.Join(paths.RootDir, "auth_sessions.json"),
		filepath.Join(exeDir, "auth_sessions.json"),
		filepath.Join(projectDir, "auth_sessions.json"),
		filepath.Join(workDir, "auth_sessions.json"),
		filepath.Join(exeDir, "backend", "auth_sessions.json"),
		filepath.Join(projectDir, "backend", "auth_sessions.json"),
		filepath.Join(workDir, "backend", "auth_sessions.json"),
	); err != nil {
		return appStoragePaths{}, err
	}

	if err := bootstrapDirectoryIfMissing(
		paths.DescriptionDir,
		filepath.Join(exeDir, "description"),
		filepath.Join(projectDir, "description"),
		filepath.Join(workDir, "description"),
	); err != nil {
		return appStoragePaths{}, err
	}

	return paths, nil
}

func resolveAppStorageRoot() (string, error) {
	baseDir := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if baseDir == "" {
		var err error
		baseDir, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("nije moguće odrediti app data folder: %w", err)
		}
	}

	rootDir := filepath.Join(baseDir, appDataFolderName)
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return "", fmt.Errorf("nije moguće kreirati app data folder: %w", err)
	}

	return rootDir, nil
}

func bootstrapFileIfMissing(destination string, sourceCandidates ...string) error {
	if info, err := os.Stat(destination); err == nil {
		if info.IsDir() {
			return fmt.Errorf("očekivan je fajl, ali postoji folder: %s", destination)
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("nije moguće kreirati direktorij za %s: %w", destination, err)
	}

	for _, source := range sourceCandidates {
		if strings.TrimSpace(source) == "" {
			continue
		}

		info, err := os.Stat(source)
		if err != nil || info.IsDir() {
			continue
		}

		if err := copyFile(source, destination, info.Mode()); err != nil {
			return fmt.Errorf("nije moguće kopirati %s u %s: %w", source, destination, err)
		}

		logEvent("storage", "file bootstrapped into app data", map[string]any{
			"destination": destination,
			"source":      source,
		})
		return nil
	}

	return nil
}

func bootstrapDirectoryIfMissing(destination string, sourceCandidates ...string) error {
	if info, err := os.Stat(destination); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("očekivan je folder, ali postoji fajl: %s", destination)
		}
		return nil
	}

	if err := os.MkdirAll(destination, 0o755); err != nil {
		return fmt.Errorf("nije moguće kreirati folder %s: %w", destination, err)
	}

	for _, source := range sourceCandidates {
		if strings.TrimSpace(source) == "" {
			continue
		}

		info, err := os.Stat(source)
		if err != nil || !info.IsDir() {
			continue
		}

		if err := copyDirectoryContents(source, destination); err != nil {
			return fmt.Errorf("nije moguće kopirati folder %s u %s: %w", source, destination, err)
		}

		logEvent("storage", "directory bootstrapped into app data", map[string]any{
			"destination": destination,
			"source":      source,
		})
		return nil
	}

	return nil
}

func copyDirectoryContents(source string, destination string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		sourcePath := filepath.Join(source, entry.Name())
		destinationPath := filepath.Join(destination, entry.Name())

		if entry.IsDir() {
			if err := os.MkdirAll(destinationPath, 0o755); err != nil {
				return err
			}
			if err := copyDirectoryContents(sourcePath, destinationPath); err != nil {
				return err
			}
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if err := copyFile(sourcePath, destinationPath, info.Mode()); err != nil {
			return err
		}
	}

	return nil
}

func copyFile(source string, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}

	return nil
}

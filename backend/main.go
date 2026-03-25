package main

import (
	"log"
	"os"
	"path/filepath"
)

func main() {
	exePath, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}

	workDir, _ := os.Getwd()
	exeDir := filepath.Dir(exePath)
	projectDir := filepath.Dir(exeDir)

	if err := initApplicationLogger([]string{workDir, projectDir, exeDir, filepath.Join(exeDir, "backend")}); err != nil {
		log.Fatal(err)
	}
	defer runtimeLogger.close()

	app, err := NewApp()
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

package main

import (
	"log"
)

func main() {
	app, cleanup, err := initApp()
	if err != nil {
		log.Fatalf("failed to initialize user-service: %v", err)
	}
	defer func() {
		cleanup()
	}()

	if err := app.Run(); err != nil {
		log.Fatalf("user-service exited with error: %v", err)
	}
}

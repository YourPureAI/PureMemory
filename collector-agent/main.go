package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"user-memory-collector/agent"
)

func main() {
	log.SetOutput(os.Stdout)
	log.Println("Initializing User Memory Collector (Windows Native)")

	agentCore, err := agent.NewAgent("user-memory.db")
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}

	err = agentCore.Start()
	if err != nil {
		log.Fatalf("Failed to start agent watchers: %v", err)
	}

	// Wait for OS shutdown signal (CTRL+C or Service Stop)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	<-sigCh
	log.Println("\nShutdown signal received.")
	agentCore.Stop()
}

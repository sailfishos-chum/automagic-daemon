package main

import (
  "log"
  "os"
  "os/signal"
  "syscall"
  "flag"
)

func main() {
  e := NewEngine()

  config_path := flag.String("c", "", "Configuration directory")
  flag.Parse()

  e.ConfigPathStatic = *config_path
  if err := e.Start(); err != nil {
    log.Fatalf("Failed to start engine: %v", err)
  }

  log.Println("Automagic daemon listening for configuration...")

  sigs := make(chan os.Signal, 1)
  signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
  <-sigs
  
  log.Println("Signal received, shutting down...")
  e.Stop()
}

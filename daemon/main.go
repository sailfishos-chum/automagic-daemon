package main

import (
  "encoding/json"
  "fmt"
  "log"
  "net"
  "os"
  "os/signal"
  "syscall"
  "flag"
)

func execFlow(secret string, flowID string, vars map[string]interface{}) {
  conn, err := net.Dial("unix", unixSocketPath)
  if err != nil {
    fmt.Fprintf(os.Stderr, "automagicd not running: %v\n", err)
    os.Exit(1)
  }
  defer conn.Close()

  dec := json.NewDecoder(conn)
  enc := json.NewEncoder(conn)

  enc.Encode(map[string]string{"secret": secret})
  var authResp SocketResponse
  if err := dec.Decode(&authResp); err != nil || !authResp.Ok {
    fmt.Fprintf(os.Stderr, "authentication failed\n")
    os.Exit(1)
  }

  enc.Encode(SocketRequest{Cmd: "execute_flow", Flow: flowID, Vars: vars})
  var resp SocketResponse
  if err := dec.Decode(&resp); err != nil || !resp.Ok {
    fmt.Fprintf(os.Stderr, "execute_flow failed: %s\n", resp.Error)
    os.Exit(1)
  }
}

func main() {
  config_path := flag.String("c", "", "Configuration directory")
  exec_flow_id := flag.String("e", "", "Execute a flow and exit")
  vars_json   := flag.String("p", "{}", "Flow variables as JSON object")
  flag.Parse()

  if *exec_flow_id != "" {
    vars := map[string]interface{}{}
    if err := json.Unmarshal([]byte(*vars_json), &vars); err != nil {
      fmt.Fprintf(os.Stderr, "invalid variables JSON: %v\n", err)
      os.Exit(1)
    }
    e := NewEngine()
    e.ConfigPathStatic = *config_path
    if *config_path != "" {
      e.SetUser(*config_path)
    } else {
      e.DiscoverUser()
    }
    e.LoadSecret()
    execFlow(e.SharedSecret, *exec_flow_id, vars)
    return
  }

  e := NewEngine()
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

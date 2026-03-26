package main

import (
  "encoding/json"
  "log"
  "net"
  "os"
  "time"
)

const socketDir = "/run/automagicd"
const unixSocketPath = socketDir + "/automagicd.sock"
const groupId = 100

func (e *Engine) addListener(conn net.Conn) {
  e.listenerMu.Lock()
  defer e.listenerMu.Unlock()
  if e.listeners == nil {
    e.listeners = make(map[net.Conn]bool)
  }
  e.listeners[conn] = true
}

func (e *Engine) removeListener(conn net.Conn) {
  e.listenerMu.Lock()
  defer e.listenerMu.Unlock()
  delete(e.listeners, conn)
}

func (e *Engine) StartUnixSocket() error {
  if err := os.MkdirAll(socketDir, 0755); err != nil {
    log.Fatalf("StartUnixSocket - Failed to create socket dir: %v", err)
    return err
  }

  uid := os.Getuid()
  os.Chown(socketDir, uid, groupId)

  if err := os.RemoveAll(unixSocketPath); err != nil {
    return err
  }

  l, err := net.Listen("unix", unixSocketPath)
  if err != nil {
    return err
  }

  os.Chown(unixSocketPath, uid, groupId)
  os.Chmod(unixSocketPath, 0660)

  log.Printf("StartUnixSocket - listening on %s", unixSocketPath)

  go func() {
    for {
      conn, err := l.Accept()
      if err != nil {
        log.Printf("StartUnixSocket - accept error: %v", err)
        continue
      }

      go e.handleUnixConn(conn)
    }
  }()

  return nil
}

func (e *Engine) StartBroadcaster() {
  go e.runBroadcaster()
}

func (e *Engine) StopBroadcaster() {
  if e.broadcastChan != nil {
    close(e.broadcastChan)
  }
}

func (e *Engine) Broadcast(msg interface{}) {
  e.listenerMu.Lock()
  defer e.listenerMu.Unlock()

  payload, err := json.Marshal(msg)
  if err != nil {
    return
  }
  payload = append(payload, '\n')

  for conn := range e.listeners {
    _, err := conn.Write(payload)
    if err != nil {
      conn.Close()
      delete(e.listeners, conn)
    }
  }
}

func (e *Engine) runBroadcaster() {
  for msg := range e.broadcastChan {
    payload, err := json.Marshal(msg)
    if err != nil {
      continue
    }
    payload = append(payload, '\n')

    e.listenerMu.Lock()
    for conn := range e.listeners {
      conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
      if _, err := conn.Write(payload); err != nil {
        conn.Close()
        delete(e.listeners, conn)
      }
    }
    e.listenerMu.Unlock()
  }
}

func (e *Engine) processSocketCommand(conn net.Conn, enc *json.Encoder, req SocketRequest) {
  switch req.Cmd {
  case "listen":
    e.addListener(conn)
    enc.Encode(SocketResponse{Ok: true})

  case "get_states":
    enc.Encode(SocketResponse{
      Ok:     true,
      States: e.GetStates(),
    })

  case "get_data":
    enc.Encode(SocketResponse{
      Ok:   true,
      Data: e.GetData(),
    })

  case "trigger":
    if req.Trigger == "" {
      enc.Encode(SocketResponse{Ok: false, Error: "missing trigger"})
      return
    }

    vars := req.Vars
    if vars == nil {
      vars = map[string]interface{}{}
    }

    log.Printf("handleUnixConn - executing trigger %s", req.Trigger)
    go e.HandleTriggerEvent(NewRunID(), req.Trigger, vars)

    enc.Encode(SocketResponse{Ok: true})

  case "execute_flow":
    if req.Flow == "" {
      enc.Encode(SocketResponse{Ok: false, Error: "missing flow"})
      return
    }

    vars := req.Vars
    if vars == nil {
      vars = map[string]interface{}{}
    }

    log.Printf("handleUnixConn - executing flow %s", req.Flow)
    go e.ExecuteFlow(NewRunID(), req.Flow, vars)

    enc.Encode(SocketResponse{Ok: true})

  case "reload":
    log.Printf("handleUnixConn - reloading")
    err := e.Reload()
    if err != nil {
      enc.Encode(SocketResponse{Ok: false, Error: err.Error()})
    } else {
      enc.Encode(SocketResponse{Ok: true})
    }

  case "version":
    enc.Encode(SocketResponse{
      Ok: true,
      Data: map[string]map[string]interface{}{
        "system": {
          "version": "1.0",
        },
      },
    })

  case "ping":
    enc.Encode(SocketResponse{
      Ok: true,
      Data: map[string]map[string]interface{}{
        "system": {
          "pong": true,
        },
      },
    })

  default:
    enc.Encode(SocketResponse{Ok: false, Error: "unknown command"})
  }
}

func (e *Engine) handleUnixConn(conn net.Conn) {
  defer func() {
    e.removeListener(conn)
    conn.Close()
  }()

  dec := json.NewDecoder(conn)
  enc := json.NewEncoder(conn)

  authenticated := false

  for {
    var raw json.RawMessage
    if err := dec.Decode(&raw); err != nil {
      break
    }

    var peek map[string]interface{}
    if err := json.Unmarshal(raw, &peek); err != nil {
      continue
    }

    if secretVal, hasSecret := peek["secret"]; hasSecret {
      secretStr, _ := secretVal.(string)

      if e.SharedSecret != "" && secretStr != e.SharedSecret {
        e.LoadSecret()
        
        if secretStr != e.SharedSecret {
          enc.Encode(SocketResponse{Ok: false, Error: "unauthorized"})
          return
        }
      }

      authenticated = true
      enc.Encode(SocketResponse{Ok: true})
      continue
    }

    cmdStr, _ := peek["cmd"].(string)
    unauthAllowed := cmdStr == "version" || cmdStr == "ping"

    if !authenticated && !unauthAllowed {
      enc.Encode(SocketResponse{Ok: false, Error: "unauthenticated"})
      return
    }

    var req SocketRequest
    if err := json.Unmarshal(raw, &req); err == nil {
      e.processSocketCommand(conn, enc, req)
    }
  }
}

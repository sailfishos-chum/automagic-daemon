package main

import (
  "fmt"
  "log"
  "context"
  "github.com/godbus/dbus/v5"
)

func (e *Engine) connectToDBus(busAddr string) (*dbus.Conn, error) {
  if busAddr == "system" {
    return dbus.SystemBus()
  } else if busAddr == "session" || busAddr == "" {
    if e.UID != "" {
      busAddr = fmt.Sprintf("unix:path=/run/user/%s/dbus/user_bus_socket", e.UID)
    } else {
      return dbus.SessionBus()
    }
  }

  conn, err := dbus.Dial(busAddr)
  if err != nil {
    return nil, err
  }

  if err = conn.Auth([]dbus.Auth{dbus.AuthExternal(e.UID)}); err != nil {
    conn.Close()
    return nil, err
  }

  if err = conn.Hello(); err != nil {
    conn.Close()
    return nil, err
  }

  return conn, nil
}

func (e *Engine) startDBusConnection(ctx context.Context, busAddr string, triggers []DataSource) {
  conn, err := e.connectToDBus(busAddr)
  if err != nil {
    log.Printf("startDBusConnection - failed to connect to bus '%s': %v", busAddr, err)
    return
  }
  
  if busAddr != "system" && busAddr != "session" {
    defer conn.Close()
  }

  for _, t := range triggers {
    if t.Interface != "" {
      match := fmt.Sprintf("type='signal',interface='%s'", t.Interface)
      if t.Path != "" {
        match += fmt.Sprintf(",path='%s'", t.Path)
      }
      if t.Signal != "" {
        match += fmt.Sprintf(",member='%s'", t.Signal)
      }

      log.Printf("[%s] Registering match: %s", busAddr, match)
      err := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, match).Store()
      if err != nil {
        log.Printf("startDBusConnection - failed to add match for %s: %v", t.Interface, err)
      }
    }
  }

  c := make(chan *dbus.Signal, 10)
  conn.Signal(c)

  log.Printf("startDBusConnection - listening on bus: %s", busAddr)

  for {
    select {
    case <-ctx.Done():
      log.Printf("startDBusConnection - shutting down connection to bus: %s", busAddr)
      conn.RemoveSignal(c)
      return
    case v := <-c:
      if v == nil {
        log.Printf("startDBusConnection - nil signal on bus: %s", busAddr)
        continue
      }
      e.handleDBusSignal(v, triggers)
    }
  }
}

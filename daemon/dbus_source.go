package main

import (
  "fmt"
  "github.com/godbus/dbus/v5"
)

func (e *Engine) fetchDBus(runID string, ds DataSource) (map[string]interface{}, error) {
  var conn *dbus.Conn
  var err error

  if ds.Address == "system" {
    conn, err = dbus.SystemBus()
  } else if ds.Address == "session" || ds.Address == "" {
    if e.UID != "" {
      bus_addr := fmt.Sprintf("unix:path=/run/user/%s/dbus/user_bus_socket", e.UID)
      conn, err = dbus.Dial(bus_addr)
    } else {
      conn, err = dbus.SessionBus()
    }
  }

  if err != nil {
    return nil, fmt.Errorf("dbus connect error: %v", err)
  }

  var resolvedArgs []interface{}
  for _, arg := range ds.Args {
    resolvedArgs = append(resolvedArgs, e.ResolveDBusArg(arg.Type, arg.Value))
  }
  obj := conn.Object(ds.Destination, dbus.ObjectPath(ds.Path))
  
  var result interface{}
  methodPath := ds.Interface + "." + ds.Method
  err = obj.Call(methodPath, 0, resolvedArgs...).Store(&result)
  
  if err != nil {
    return nil, fmt.Errorf("dbus call failed (%s): %v", methodPath, err)
  }

  return map[string]interface{}{
    "arg0": result,
  }, nil
}

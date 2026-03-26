package main

import (
  "context"
  "fmt"
  "log"
  "strings"
  "github.com/godbus/dbus/v5"
)

func (e *Engine) StartDBusTriggers() {
  if e.dbus_cancels == nil {
    e.dbus_cancels = make(map[string]context.CancelFunc)
  }

  busGroups := make(map[string][]DataSource)

  log.Printf("StartDBusTriggers - checking %d data sources", len(e.dataSources))

  for _, t := range e.dataSources {
    if !t.Enabled || !t.Trigger || t.Type != "dbus" {
      continue
    }

    log.Printf("StartDBusTriggers - found enabled trigger: %s (bus: %s)", t.ID, t.Address)

    address := t.Address
    if address == "" {
      address = "session" 
    }

    busGroups[address] = append(busGroups[address], t)
  }

  log.Printf("StartDBusTriggers - grouped into %d unique bus connections", len(busGroups))

  for bus_addr, bus_triggers := range busGroups {
    ctx, cancel := context.WithCancel(context.Background())
    e.dbus_cancels[bus_addr] = cancel

    log.Printf("StartDBusTriggers - launching connection for: %s", bus_addr)
    go e.startDBusConnection(ctx, bus_addr, bus_triggers)
  }
}

func (e *Engine) handleDBusSignal(v *dbus.Signal, triggers []DataSource) {
  for _, t := range triggers {
    if t.Interface != "" && !strings.HasPrefix(v.Name, t.Interface) {
      continue
    }

    if t.Signal != "" && !strings.HasSuffix(v.Name, "."+t.Signal) {
      continue
    }

    vars := make(map[string]interface{})
    vars_out := make(map[string]interface{})

    vars["signal"] = v.Name
    vars["_path"] = string(v.Path)
    
    for i, arg := range v.Body {
      if variant, ok := arg.(dbus.Variant); ok {
        vars[fmt.Sprintf("arg%d", i)] = variant.Value()
      } else {
        vars[fmt.Sprintf("arg%d", i)] = arg
      }
    }

    if !matchFilters(t.Filters, vars) {
      continue
    }

    log.Printf("handleDBusSignal - trigger '%s' event from %s", t.ID, v.Name)

    if len(t.Transformations) > 0 {
      if !applyTransformations(t.Transformations, e.ValueMaps, vars, vars_out) {
        log.Printf("handleDBusSignal - trigger '%s' transformations failed", t.ID)
        continue
      }
    }
    
    e.HandleTriggerEvent(NewRunID(), t.ID, vars_out)
  }
}

func (e *Engine) StopDBusTriggers() {
  if e.dbus_cancels == nil {
    return
  }
  for busAddr, cancel := range e.dbus_cancels {
    log.Printf("Signaling D-Bus connection '%s' to shut down...", busAddr)
    cancel()
  }
  e.dbus_cancels = nil
}

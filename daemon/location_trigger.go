package main

import (
  "context"
  "log"
  "math"
  "strings"
  "time"
  "sync"
  "github.com/godbus/dbus/v5"
)

var (
  locationFired   = make(map[string]time.Time)
  locationFiredMu sync.Mutex
)

func (e *Engine) StartLocationTriggers() {
  if e.location_cancel != nil {
    return
  }

  hasLocationTrigger := false
  var minInterval int32 = 0

  for _, t := range e.dataSources {
    if t.Enabled && t.Trigger && t.Type == "location" {
      hasLocationTrigger = true
      
      if t.Interval != "" {
        if d, err := time.ParseDuration(t.Interval); err == nil {
          sec := int32(d.Seconds())
          if sec > 0 && (minInterval == 0 || sec < minInterval) {
            minInterval = sec
          }
        }
      }
    }
  }

  if !hasLocationTrigger {
    return
  }

  ctx, cancel := context.WithCancel(context.Background())
  e.location_cancel = cancel

  go e.runLocationListener(ctx, minInterval)
}

func (e *Engine) runLocationListener(ctx context.Context, interval int32) {
  conn, err := e.connectToDBus("session")
  if err != nil {
    log.Printf("runLocationListener - connect error: %v", err)
    return
  }
  defer conn.Close()

  master := conn.Object("org.freedesktop.Geoclue.Master", "/org/freedesktop/Geoclue/Master")
  var clientPath dbus.ObjectPath
  err = master.Call("org.freedesktop.Geoclue.Master.Create", 0).Store(&clientPath)
  if err != nil {
    log.Printf("runLocationListener - master create error: %v", err)
    return
  }

  client := conn.Object("org.freedesktop.Geoclue.Master", clientPath)
  client.Call("org.freedesktop.Geoclue.AddReference", 0)
  client.Call("org.freedesktop.Geoclue.MasterClient.SetRequirements", 0, int32(0), int32(0), true, int32(1023))
  client.Call("org.freedesktop.Geoclue.MasterClient.PositionStart", 0)

  hybris := conn.Object("org.freedesktop.Geoclue.Providers.Hybris", "/org/freedesktop/Geoclue/Providers/Hybris")
  hybris.Call("org.freedesktop.Geoclue.AddReference", 0)

  opts := map[string]dbus.Variant{
    "UpdateInterval": dbus.MakeVariant(interval),
  }
  hybris.Call("org.freedesktop.Geoclue.SetOptions", 0, opts)

  rule := "type='signal',interface='org.freedesktop.Geoclue.Position',member='PositionChanged',path='/org/freedesktop/Geoclue/Providers/Hybris'"
  conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, rule)

  c := make(chan *dbus.Signal, 10)
  conn.Signal(c)

  log.Printf("runLocationListener - listening for location updates every %d s", interval)

  for {
    select {
    case <-ctx.Done():
      hybris.Call("org.freedesktop.Geoclue.RemoveReference", 0)
      client.Call("org.freedesktop.Geoclue.MasterClient.PositionStop", 0)
      client.Call("org.freedesktop.Geoclue.RemoveReference", 0)
      conn.BusObject().Call("org.freedesktop.DBus.RemoveMatch", 0, rule)
      return
    case v := <-c:
      e.handleLocationSignal(v)
    }
  }
}

func (e *Engine) handleLocationSignal(v *dbus.Signal) {
  if len(v.Body) < 6 {
    return
  }

  timestamp, _ := v.Body[1].(int32)
  latitude, okLat := v.Body[2].(float64)
  longitude, okLon := v.Body[3].(float64)
  altitude, _ := v.Body[4].(float64)

  if !okLat || !okLon || math.IsNaN(latitude) || math.IsNaN(longitude) {
    return
  }

  var horizAcc, vertAcc float64
  if acc, ok := v.Body[5].(dbus.Variant); ok {
    if val, ok := acc.Value().([]interface{}); ok && len(val) >= 3 {
      horizAcc, _ = val[1].(float64)
      vertAcc, _ = val[2].(float64)
    }
  }

  provider := "unknown"
  path := string(v.Path)
  if path != "" {
    if strings.Contains(path, "Hybris") {
      provider = "gps"
    } else if strings.Contains(path, "Mlsdb") {
      provider = "network"
    }
  }

  e.lastLocation.mu.Lock()
  e.lastLocation.latitude = latitude
  e.lastLocation.longitude = longitude
  e.lastLocation.altitude = altitude
  e.lastLocation.accuracy = horizAcc
  e.lastLocation.verticalAccuracy = vertAcc
  e.lastLocation.timestamp = timestamp
  e.lastLocation.provider = provider
  e.lastLocation.mu.Unlock()

  vars := map[string]interface{}{
    "latitude":          latitude,
    "longitude":         longitude,
    "altitude":          altitude,
    "accuracy":          horizAcc,
    "vertical_accuracy": vertAcc,
    "timestamp":         timestamp,
    "provider":          provider,
  }

  for _, t := range e.dataSources {
    if t.Enabled && t.Trigger && t.Type == "location" {
      if t.Interval != "" {
        if d, err := time.ParseDuration(t.Interval); err == nil {
          locationFiredMu.Lock()
          lastFired := locationFired[t.ID]
          locationFiredMu.Unlock()

          if time.Since(lastFired) < d {
            continue
          }
        }
      }

      if matchFilters(t.Filters, vars) {
        log.Printf("handleLocationSignal - trigger '%s' event (latitude: %f, longitude: %f)", t.ID, latitude, longitude)

        vars_out := make(map[string]interface{})

        if len(t.Transformations) > 0 {
          if !applyTransformations(t.Transformations, e.ValueMaps, vars, vars_out) {
            log.Printf("handleLocationSignal - trigger '%s' transformations failed", t.ID)
            continue
          }
        } else {
          for k, val := range vars {
            vars_out[k] = val
          }
        }

        locationFiredMu.Lock()
        locationFired[t.ID] = time.Now()
        locationFiredMu.Unlock()

        e.HandleTriggerEvent(NewRunID(), t.ID, vars_out)
      }
    }
  }
}

func (e *Engine) StopLocationTriggers() {
  if e.location_cancel != nil {
    log.Println("Signaling location listener to shut down...")
    e.location_cancel()
    e.location_cancel = nil
  }
}
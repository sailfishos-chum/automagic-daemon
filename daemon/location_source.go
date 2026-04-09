package main

import (
  "log"
  "context"
  "fmt"
  "math"
  "strings"
  "time"
  "github.com/godbus/dbus/v5"
)

func (e *Engine) fetchLocation(runID string, ds DataSource) (map[string]interface{}, error) {
  e.lastLocation.mu.RLock()
  lat := e.lastLocation.latitude
  lon := e.lastLocation.longitude
  ts := e.lastLocation.timestamp
  e.lastLocation.mu.RUnlock()

if lat != 0 && lon != 0 {
    maxAgeSecs := int64(60)
    
    if ds.CacheTTL != "" {
      if d, err := time.ParseDuration(ds.CacheTTL); err == nil {
        maxAgeSecs = int64(d.Seconds())
      }
    }

    age := time.Now().Unix() - int64(ts)
    if age < maxAgeSecs {
      lm := e.getLocationMap()
      log.Printf("fetchLocation - source '%s' using cached values - timestamp: %d, latitude: %f, longitude: %f ", ds.ID, lm["timestamp"], lm["latitude"], lm["longitude"])

      return e.getLocationMap(), nil
    }
  }

  timeout := 30 * time.Second
  if ds.Timeout != "" {
    if parsed, err := time.ParseDuration(ds.Timeout); err == nil {
      timeout = parsed
    }
  }

  log.Printf("fetchLocation - source '%s' acquiring position - timeout: %f s", ds.ID, timeout.Seconds())

  ctx, cancel := context.WithTimeout(context.Background(), timeout)
  defer cancel()

  conn, err := e.connectToDBus("session")
  if err != nil {
    return nil, fmt.Errorf("dbus connect error: %v", err)
  }
  defer conn.Close()

  master := conn.Object("org.freedesktop.Geoclue.Master", "/org/freedesktop/Geoclue/Master")
  var clientPath dbus.ObjectPath
  if err = master.Call("org.freedesktop.Geoclue.Master.Create", 0).Store(&clientPath); err != nil {
    return nil, fmt.Errorf("geoclue master create error: %v", err)
  }

  client := conn.Object("org.freedesktop.Geoclue.Master", clientPath)
  client.Call("org.freedesktop.Geoclue.AddReference", 0)
  client.Call("org.freedesktop.Geoclue.MasterClient.SetRequirements", 0, int32(0), int32(0), true, int32(1023))
  client.Call("org.freedesktop.Geoclue.MasterClient.PositionStart", 0)

  hybris := conn.Object("org.freedesktop.Geoclue.Providers.Hybris", "/org/freedesktop/Geoclue/Providers/Hybris")
  hybris.Call("org.freedesktop.Geoclue.AddReference", 0)

  opts := map[string]dbus.Variant{"UpdateInterval": dbus.MakeVariant(int32(0))}
  hybris.Call("org.freedesktop.Geoclue.SetOptions", 0, opts)

  rule := "type='signal',interface='org.freedesktop.Geoclue.Position',member='PositionChanged',path='/org/freedesktop/Geoclue/Providers/Hybris'"
  conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, rule)

  c := make(chan *dbus.Signal, 10)
  conn.Signal(c)

  for {
    select {
    case <-ctx.Done():
      hybris.Call("org.freedesktop.Geoclue.RemoveReference", 0)
      client.Call("org.freedesktop.Geoclue.MasterClient.PositionStop", 0)
      client.Call("org.freedesktop.Geoclue.RemoveReference", 0)
      conn.BusObject().Call("org.freedesktop.DBus.RemoveMatch", 0, rule)
      return nil, fmt.Errorf("location fetch timed out")

    case v := <-c:
      if len(v.Body) < 6 {
        continue
      }

      latitude, okLat := v.Body[2].(float64)
      longitude, okLon := v.Body[3].(float64)

      if !okLat || !okLon || math.IsNaN(latitude) || math.IsNaN(longitude) {
        continue
      }

      hybris.Call("org.freedesktop.Geoclue.RemoveReference", 0)
      client.Call("org.freedesktop.Geoclue.MasterClient.PositionStop", 0)
      client.Call("org.freedesktop.Geoclue.RemoveReference", 0)
      conn.BusObject().Call("org.freedesktop.DBus.RemoveMatch", 0, rule)

      timestamp, _ := v.Body[1].(int32)
      altitude, _ := v.Body[4].(float64)
      var horizAcc, vertAcc float64
      if acc, ok := v.Body[5].(dbus.Variant); ok {
        if val, ok := acc.Value().([]interface{}); ok && len(val) >= 3 {
          horizAcc, _ = val[1].(float64)
          vertAcc, _ = val[2].(float64)
        }
      }

      provider := "unknown"
      if strings.Contains(string(v.Path), "Hybris") {
        provider = "gps"
      } else if strings.Contains(string(v.Path), "Mlsdb") {
        provider = "network"
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

      return e.getLocationMap(), nil
    }
  }
}

func (e *Engine) getLocationMap() map[string]interface{} {
  e.lastLocation.mu.RLock()
  defer e.lastLocation.mu.RUnlock()

  return map[string]interface{}{
    "latitude":          e.lastLocation.latitude,
    "longitude":         e.lastLocation.longitude,
    "altitude":          e.lastLocation.altitude,
    "accuracy":          e.lastLocation.accuracy,
    "vertical_accuracy": e.lastLocation.verticalAccuracy,
    "timestamp":         e.lastLocation.timestamp,
    "provider":          e.lastLocation.provider,
  }
}

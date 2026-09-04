package main

import (
  "context"
  "encoding/binary"
  "fmt"
  "log"
  "os"
  "strings"
)

const inputDevicesByPath = "/dev/input/by-path/"

// event type/code constants we care about (linux/input-event-codes.h)
const evKey = 1

var inputKeyNames = map[uint16]string{
  113: "KEY_MUTE",
  114: "KEY_VOLUMEDOWN",
  115: "KEY_VOLUMEUP",
  116: "KEY_POWER",
  163: "KEY_NEXTSONG",
  164: "KEY_PLAYPAUSE",
  165: "KEY_PREVIOUSSONG",
  166: "KEY_STOPCD",
  200: "KEY_PLAYCD",
  201: "KEY_PAUSECD",
  212: "KEY_CAMERA",
  248: "KEY_MICMUTE",
}

func inputKeyName(code uint16) string {
  if name, ok := inputKeyNames[code]; ok {
    return name
  }
  return fmt.Sprintf("KEY_%d", code)
}

// splitDeviceClasses parses a comma-separated "path" field into individual
// device classes - e.g. "gpio_keys, power-on" for hardware that splits
// buttons across multiple input devices (volume-up/power sharing the PMIC's
// power-on driver while volume-down is on the generic gpio-keys driver is a
// real example, not hypothetical).
func splitDeviceClasses(raw string) []string {
  var classes []string
  for _, part := range strings.Split(raw, ",") {
    part = strings.TrimSpace(part)
    if part != "" {
      classes = append(classes, part)
    }
  }
  return classes
}

// resolveInputDevicePath finds a /dev/input/eventN node whose by-path name
// contains deviceClass, matching harbour-s1p's device_class convention
// (e.g. "gpio_keys" for hardware buttons, "sound" for the headset jack).
func resolveInputDevicePath(deviceClass string) (string, error) {
  entries, err := os.ReadDir(inputDevicesByPath)
  if err != nil {
    return "", fmt.Errorf("could not read %s: %v", inputDevicesByPath, err)
  }

  for _, entry := range entries {
    if strings.Contains(entry.Name(), deviceClass) {
      return inputDevicesByPath + entry.Name(), nil
    }
  }

  return "", fmt.Errorf("no input device found matching class %q", deviceClass)
}

func (e *Engine) StartInputTriggers() {
  if e.input_device_cancels == nil {
    e.input_device_cancels = make(map[string]context.CancelFunc)
  }

  for _, ds := range e.dataSources {
    if !ds.Enabled || !ds.Trigger || ds.Type != "input_device" {
      continue
    }

    classes := splitDeviceClasses(ds.Path)
    if len(classes) == 0 {
      log.Printf("StartInputTriggers - '%s': no device class configured", ds.ID)
      continue
    }

    ctx, cancel := context.WithCancel(context.Background())
    e.input_device_cancels[ds.ID] = cancel

    for _, class := range classes {
      go e.watchInputDevice(ctx, ds, class)
    }
  }
}

func (e *Engine) watchInputDevice(ctx context.Context, ds DataSource, deviceClass string) {
  devicePath, err := resolveInputDevicePath(deviceClass)
  if err != nil {
    log.Printf("watchInputDevice - '%s': %v", ds.ID, err)
    return
  }

  f, err := os.Open(devicePath)
  if err != nil {
    log.Printf("watchInputDevice - '%s': could not open %s: %v", ds.ID, devicePath, err)
    return
  }
  defer f.Close()

  go func() {
    <-ctx.Done()
    f.Close()
  }()

  log.Printf("watchInputDevice - '%s': reading %s", ds.ID, devicePath)

  buf := make([]byte, 24)
  for {
    n, err := f.Read(buf)
    if err != nil {
      if ctx.Err() != nil {
        log.Printf("watchInputDevice - '%s': shutting down", ds.ID)
        return
      }
      log.Printf("watchInputDevice - '%s': read error: %v", ds.ID, err)
      return
    }
    if n < 24 {
      continue
    }

    evType := binary.LittleEndian.Uint16(buf[16:18])
    if evType != evKey {
      continue
    }

    evCode := binary.LittleEndian.Uint16(buf[18:20])
    evValue := int32(binary.LittleEndian.Uint32(buf[20:24]))

    vars := map[string]interface{}{
      "device":    devicePath,
      "key_code":  int(evCode),
      "key_name":  inputKeyName(evCode),
      "key_value": int(evValue),
    }

    if !matchFilters(ds.Filters, vars) {
      continue
    }

    runID := NewRunID()
    log.Printf("[%s] watchInputDevice - '%s': %s (value %d)", runID, ds.ID, inputKeyName(evCode), evValue)

    vars_out := make(map[string]interface{})
    if len(ds.Transformations) > 0 {
      if !applyTransformations(ds.Transformations, e.ValueMaps, vars, vars_out) {
        log.Printf("[%s] watchInputDevice - '%s': transformations failed", runID, ds.ID)
        continue
      }
    }

    e.HandleTriggerEvent(runID, ds.ID, vars_out)
  }
}

func (e *Engine) StopInputTriggers() {
  if e.input_device_cancels == nil {
    return
  }
  for id, cancel := range e.input_device_cancels {
    log.Printf("StopInputTriggers - stopping '%s'", id)
    cancel()
  }
  e.input_device_cancels = nil
}

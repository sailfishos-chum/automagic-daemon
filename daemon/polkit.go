package main

import (
  "context"
  "encoding/json"
  "fmt"
  "net"
  "os"
  "strconv"
  "strings"
  "syscall"
  "time"

  "github.com/godbus/dbus/v5"
)

const polkitActionUpdatePrivilegedConfig = "app.qml.automagic.update-privileged-config"

type polkitSubject struct {
  Kind    string
  Details map[string]dbus.Variant
}

type polkitAuthResult struct {
  IsAuthorized bool
  IsChallenge  bool
  Details      map[string]string
}

// getPeerCredentials returns the pid/uid of the process on the other end of
// a Unix domain socket connection, via SO_PEERCRED.
func getPeerCredentials(conn net.Conn) (pid int32, uid uint32, err error) {
  unixConn, ok := conn.(*net.UnixConn)
  if !ok {
    return 0, 0, fmt.Errorf("connection is not a unix socket")
  }

  raw, err := unixConn.SyscallConn()
  if err != nil {
    return 0, 0, err
  }

  var ucred *syscall.Ucred
  var ctrlErr error
  err = raw.Control(func(fd uintptr) {
    ucred, ctrlErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
  })
  if err != nil {
    return 0, 0, err
  }
  if ctrlErr != nil {
    return 0, 0, ctrlErr
  }

  return ucred.Pid, uint32(ucred.Uid), nil
}

// getProcessStartTime returns a process's start time (field 22 of
// /proc/<pid>/stat, in clock ticks since boot) - polkit uses this to
// disambiguate a pid from a since-reused one with the same number.
func getProcessStartTime(pid int32) (uint64, error) {
  data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
  if err != nil {
    return 0, err
  }

  // The executable name is wrapped in parens and may itself contain spaces
  // or parens, so find the LAST ")" and treat everything after it as the
  // remaining space-separated fields, starting at field 3.
  end := strings.LastIndex(string(data), ")")
  if end < 0 || end+2 >= len(data) {
    return 0, fmt.Errorf("unexpected /proc/%d/stat format", pid)
  }

  fields := strings.Fields(string(data[end+2:]))
  const startTimeFieldIndex = 22 - 3 // fields[0] here is field 3 overall
  if len(fields) <= startTimeFieldIndex {
    return 0, fmt.Errorf("unexpected /proc/%d/stat field count", pid)
  }

  return strconv.ParseUint(fields[startTimeFieldIndex], 10, 64)
}

// checkPolkitAuthorization asks PolicyKit whether pid/uid is authorized for
// actionId, triggering the interactive authentication agent if needed. This
// blocks until the user responds (or the timeout elapses).
func (e *Engine) checkPolkitAuthorization(actionId string, pid int32, uid uint32) (bool, error) {
  startTime, err := getProcessStartTime(pid)
  if err != nil {
    return false, fmt.Errorf("could not determine process start time: %v", err)
  }

  conn, err := dbus.SystemBus()
  if err != nil {
    return false, fmt.Errorf("could not connect to system bus: %v", err)
  }

  subject := polkitSubject{
    Kind: "unix-process",
    Details: map[string]dbus.Variant{
      "pid":        dbus.MakeVariant(uint32(pid)),
      "start-time": dbus.MakeVariant(startTime),
      "uid":        dbus.MakeVariant(int32(uid)),
    },
  }

  obj := conn.Object("org.freedesktop.PolicyKit1", dbus.ObjectPath("/org/freedesktop/PolicyKit1/Authority"))

  ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
  defer cancel()

  var result polkitAuthResult
  const allowUserInteraction = uint32(1)

  err = obj.CallWithContext(ctx, "org.freedesktop.PolicyKit1.Authority.CheckAuthorization", 0,
    subject, actionId, map[string]string{}, allowUserInteraction, "",
  ).Store(&result)

  if err != nil {
    return false, fmt.Errorf("CheckAuthorization failed: %v", err)
  }

  return result.IsAuthorized, nil
}

// writePrivilegedConfig writes privileged.json as root:root, mode 0644 -
// only called after checkPolkitAuthorization has already confirmed consent.
func (e *Engine) writePrivilegedConfig(allowedRunAs []string) error {
  path := e.ConfigPath + "/privileged.json"

  cfg := PrivilegedConfig{Version: 1, AllowedRunAs: allowedRunAs}
  data, err := json.MarshalIndent(cfg, "", "  ")
  if err != nil {
    return err
  }

  if err := os.WriteFile(path, data, 0644); err != nil {
    return err
  }

  return os.Chmod(path, 0644)
}

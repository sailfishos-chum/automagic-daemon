package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func (e *Engine) fetchShell(runID string, ds DataSource, params map[string]interface{}) (map[string]interface{}, error) {
  if ds.Command == "" {
    return nil, fmt.Errorf("shell source '%s' requires a command", ds.ID)
  }

  cmdStr := expandByTemplate(ds.Command, params)
  cmd := exec.Command("sh", "-c", cmdStr)

  runAsUser := e.SessionUser
  if ds.RunAs != "" && (e.SessionUser == nil || ds.RunAs != e.SessionUser.Username) {
    privUser, err := e.resolvePrivilegedUser(ds.RunAs)
    if err != nil {
      log.Printf("[%s] fetchShell - run_as %q denied: %v", runID, ds.RunAs, err)
      return nil, fmt.Errorf("run_as %q denied: %v", ds.RunAs, err)
    }
    runAsUser = privUser
  }

  if runAsUser != nil {
    uid, _ := strconv.Atoi(runAsUser.Uid)
    gid, _ := strconv.Atoi(runAsUser.Gid)

    cmd.SysProcAttr = &syscall.SysProcAttr{
      Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)},
    }

    cmd.Env = os.Environ()
    cmd.Env = append(cmd.Env,
      "HOME="+runAsUser.HomeDir,
      "USER="+runAsUser.Username,
      "LOGNAME="+runAsUser.Username,
      fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/dbus/user_bus_socket", uid),
      "PATH=/usr/local/bin:/bin:/usr/bin:/usr/local/sbin:/usr/sbin",
    )
  }

  log.Printf("[%s] fetchShell - executing: %s", runID, cmdStr)

  var stdout, stderr bytes.Buffer
  cmd.Stdout = &stdout
  cmd.Stderr = &stderr

  err := cmd.Run()
  if stderr.Len() > 0 {
    log.Printf("[%s] fetchShell - stderr: %s", runID, stderr.String())
  }
  if err != nil {
    return nil, fmt.Errorf("shell command failed: %v", err)
  }

  format := ds.Format
  if format == "" {
    format = "raw"
  }

  data, err := parsePayload(format, stdout.Bytes(), ds.Pattern, ds.Delimiter)
  if err != nil {
    return nil, fmt.Errorf("parse error from shell source '%s': %v", ds.ID, err)
  }

  return data, nil
}

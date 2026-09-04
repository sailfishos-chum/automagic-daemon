package main

import (
  "encoding/json"
  "fmt"
  "os"
  "os/user"
  "syscall"
)

type PrivilegedConfig struct {
  Version      int      `json:"version"`
  AllowedRunAs []string `json:"allowed_run_as"`
}

// resolvePrivilegedUser authorizes running as a user other than the session
// user. It only succeeds if <config path>/privileged.json exists, is owned
// by root, is not writable by group or other, and explicitly lists runAsUser
// in allowed_run_as - defaultuser (who the daemon otherwise runs actions as)
// cannot forge any of those, only delete the file, which fails closed.
func (e *Engine) resolvePrivilegedUser(runAsUser string) (*user.User, error) {
  path := e.ConfigPath + "/privileged.json"

  info, err := os.Stat(path)
  if err != nil {
    return nil, fmt.Errorf("privileged config not found: %v", err)
  }

  stat, ok := info.Sys().(*syscall.Stat_t)
  if !ok {
    return nil, fmt.Errorf("could not read privileged config ownership")
  }
  if stat.Uid != 0 {
    return nil, fmt.Errorf("privileged config %s is not owned by root (owned by uid %d) - refusing", path, stat.Uid)
  }
  if info.Mode().Perm()&0022 != 0 {
    return nil, fmt.Errorf("privileged config %s is writable by group or other (mode %s) - refusing", path, info.Mode().Perm())
  }

  data, err := os.ReadFile(path)
  if err != nil {
    return nil, fmt.Errorf("could not read privileged config: %v", err)
  }

  var cfg PrivilegedConfig
  if err := json.Unmarshal(data, &cfg); err != nil {
    return nil, fmt.Errorf("could not parse privileged config: %v", err)
  }

  allowed := false
  for _, u := range cfg.AllowedRunAs {
    if u == runAsUser {
      allowed = true
      break
    }
  }
  if !allowed {
    return nil, fmt.Errorf("user %q is not in privileged config's allowed_run_as list", runAsUser)
  }

  return user.Lookup(runAsUser)
}

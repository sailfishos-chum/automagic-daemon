package main

import (
  "log"
  "os"
  "context"
  "github.com/fsnotify/fsnotify"
)

func (e *Engine) StartFileTriggers() {
  if e.file_watchers == nil {
    e.file_watchers = make(map[string]*FileWatcher)
  }

  if e.file_watcher_cancels == nil {
    e.file_watcher_cancels = make(map[string]context.CancelFunc)
  }

  for _, t := range e.dataSources {
    if !t.Enabled || !t.Trigger || t.Type != "file" {
      continue
    }

    file_path := t.Path

    if _, ok := e.file_watchers[file_path]; !ok {
      e.file_watchers[file_path] = &FileWatcher{
        File: file_path,
      }
    }

    e.file_watchers[file_path].Triggers = append(e.file_watchers[file_path].Triggers, t)
  }

  log.Printf("StartFileTriggers - file_watchers: %d", len(e.file_watchers))

  for file_id, b := range e.file_watchers {
    log.Printf("StartFileTriggers - file_watcher: %v, triggers: %d", b.File, len(b.Triggers))
    
    ctx, cancel := context.WithCancel(context.Background())
    e.file_watcher_cancels[file_id] = cancel

    go e.startFileTrigger(ctx, b)
  }
}

func (e *Engine) startFileTrigger(ctx context.Context, fw *FileWatcher) {
  watcher, err := fsnotify.NewWatcher()
  if err != nil {
    log.Printf("startFileTrigger init error: %v", err)
    return
  }
  defer watcher.Close()

  err = watcher.Add(fw.File)
  if err != nil {
    log.Printf("startFileTrigger add error for %s: %v", fw.File, err)
    return
  }

  for {
    select {
    case <-ctx.Done():
      return
    case event, ok := <-watcher.Events:
      log.Printf("startFileTrigger event: %s, ok: %v", event, ok)
      if !ok {
        return
      }

      info, err := os.Stat(fw.File)
      is_dir := (err == nil && info.IsDir())
      
      for _, t := range fw.Triggers {
        runID := NewRunID()
        vars_out := make(map[string]interface{})
        vars := make(map[string]interface{})

        if !is_dir && fw.File == event.Name {
          vars, err = e.fetchFile(runID, t)
          if err != nil {
            log.Printf("[%s] startFileTrigger - fetchFile error: %v", runID, err)
          }
        }

        vars["path"] = fw.File
        vars["file"] = event.Name
        vars["write"] = event.Has(fsnotify.Write)
        vars["create"] = event.Has(fsnotify.Create)
        vars["remove"] = event.Has(fsnotify.Remove)
        vars["rename"] = event.Has(fsnotify.Rename)
        vars["chmod"] = event.Has(fsnotify.Chmod)

        if !matchFilters(t.Filters, vars) {
          return
        }

        if len(t.Transformations) > 0 {
          if !applyTransformations(t.Transformations, e.ValueMaps, vars, vars_out) {
            log.Printf("[%s] startFileTrigger - trigger '%s' transformations failed", runID, t.ID)
            return
          }
        }

        e.HandleTriggerEvent(runID, t.ID, vars_out)
      }
      
    case err, ok := <-watcher.Errors:
      if !ok {
        return
      }
      log.Printf("watcher error for %s: %v", fw.File, err)
    }
  }
}

func (e *Engine) StopFileTriggers() {
  if e.file_watcher_cancels == nil {
    return
  }
  
  for file_name, cancel := range e.file_watcher_cancels {
    log.Printf("Signaling file watcher to shut down: %s", file_name)
    cancel()
  }

  e.file_watcher_cancels = nil
  e.file_watchers = nil
}

func (e *Engine) fetchFile(runID string, ds DataSource) (map[string]interface{}, error) {
  vars := make(map[string]interface{})
  
  info, err := os.Stat(ds.Path)
  if os.IsNotExist(err) {
    vars["exists"] = false
    return vars, nil
  } else if err != nil {
    return nil, err
  }

  vars["exists"] = true
  vars["size"] = info.Size()
  vars["base_name"] = info.Name()
  vars["is_directory"] = info.IsDir()
  vars["modify_time"] = info.ModTime().Unix()
  vars["mode"] = info.Mode().String()
  vars["permissions"] = uint32(info.Mode().Perm())

  format := ds.Format
  if format == "" {
    format = "raw"
  }

  if format == "none" || format == "null" {
    return vars, nil
  }

  data, err := os.ReadFile(ds.Path)
  if err != nil {
    return nil, err
  }

  payload, err := parsePayload(format, data, ds.Pattern, ds.Delimiter)

  return payload, err
}

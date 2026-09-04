package main

import (
  "context"
  "log"
  "time"
)

func (e *Engine) StartHTTPTriggers() {
  if e.http_trigger_cancels == nil {
    e.http_trigger_cancels = make(map[string]context.CancelFunc)
  }

  for _, ds := range e.dataSources {
    if !ds.Enabled || !ds.Trigger || ds.Type != "http" {
      continue
    }

    log.Printf("StartHTTPTriggers - launching loop for: %s", ds.ID)
    ctx, cancel := context.WithCancel(context.Background())
    e.http_trigger_cancels[ds.ID] = cancel
    go e.startHTTPTriggerLoop(ctx, ds)
  }
}

func (e *Engine) startHTTPTriggerLoop(ctx context.Context, ds DataSource) {
  var interval time.Duration
  if ds.Interval != "" {
    if d, err := time.ParseDuration(ds.Interval); err == nil {
      interval = d
    }
  }

  for {
    select {
    case <-ctx.Done():
      log.Printf("startHTTPTriggerLoop - shutting down: %s", ds.ID)
      return
    default:
    }

    params := e.GetStates()
    runID := NewRunID()

    raw, err := e.fetchHTTP(runID, ds, params)
    if err != nil {
      log.Printf("[%s] startHTTPTriggerLoop - '%s' request failed: %v", runID, ds.ID, err)
      delay := interval
      if delay < 5*time.Second {
        delay = 5 * time.Second
      }
      select {
      case <-ctx.Done():
        return
      case <-time.After(delay):
      }
      continue
    }

    if matchFilters(ds.Filters, raw) {
      vars_out := make(map[string]interface{})
      if len(ds.Transformations) > 0 {
        if !applyTransformations(ds.Transformations, e.ValueMaps, raw, vars_out) {
          log.Printf("[%s] startHTTPTriggerLoop - '%s' transformations failed, skipping", runID, ds.ID)
        } else {
          e.HandleTriggerEvent(runID, ds.ID, vars_out)
        }
      } else {
        e.HandleTriggerEvent(runID, ds.ID, raw)
      }
    }

    if interval > 0 {
      select {
      case <-ctx.Done():
        return
      case <-time.After(interval):
      }
    }
  }
}

func (e *Engine) StopHTTPTriggers() {
  if e.http_trigger_cancels == nil {
    return
  }
  for id, cancel := range e.http_trigger_cancels {
    log.Printf("StopHTTPTriggers - stopping '%s'", id)
    cancel()
  }
  e.http_trigger_cancels = nil
}

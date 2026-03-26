package main

import (
  "context"
  "log"
  "time"
)

func (e *Engine) StartTimerTriggers() {
  if e.timer_cancels == nil {
    e.timer_cancels = make(map[string]context.CancelFunc)
  }

  log.Printf("StartTimerTriggers - checking %d data sources", len(e.dataSources))

  for _, t := range e.dataSources {
    if !t.Enabled || !t.Trigger || t.Type != "timer" {
      continue
    }

    log.Printf("StartTimerTriggers - found enabled timer: %s (interval: %s)", t.ID, t.Interval)

    ctx, cancel := context.WithCancel(context.Background())
    e.timer_cancels[t.ID] = cancel

    log.Printf("StartTimerTriggers - launching loop for: %s", t.ID)
    go e.startTimerLoop(ctx, t)
  }
}

func (e *Engine) startTimerLoop(ctx context.Context, t DataSource) {
  duration, _ := time.ParseDuration(t.Interval)
 
  if t.InitialDelay != "" {
    delay, err := time.ParseDuration(t.InitialDelay)
    if err != nil {
      log.Printf("startTimerLoop - failed to parse initial_delay for timer '%s': %v", t.ID, err)
    } else {
      log.Printf("startTimerLoop - timer '%s' waiting for initial_delay: %s", t.ID, t.InitialDelay)
      
      select {
      case <-ctx.Done():
          return
      case <-time.After(delay):
          log.Printf("startTimerLoop - initial_delay finished, firing timer '%s' for startup", t.ID)
          e.handleTimerTick(time.Now(), t)
      }
    }
  }

  if duration == time.Duration(0) {
    return
  }

  ticker := time.NewTicker(duration)
  defer ticker.Stop()

  log.Printf("startTimerLoop - listening for timer: %s (every %s)", t.ID, t.Interval)

  for {
    select {
    case <-ctx.Done():
      log.Printf("startTimerLoop - shutting down timer: %s", t.ID)
      return
    case tickTime := <-ticker.C:
      e.handleTimerTick(tickTime, t)
    }
  }
}

func (e *Engine) handleTimerTick(tickTime time.Time, t DataSource) {
  vars := make(map[string]interface{})
  vars_out := make(map[string]interface{})

  vars["trigger_time"] = tickTime.Unix()
  vars["trigger_source"] = t.ID

  if !matchFilters(t.Filters, vars) {
    return
  }

  log.Printf("handleTimerTick - trigger '%s' fired", t.ID)

  if len(t.Transformations) > 0 {
    if !applyTransformations(t.Transformations, e.ValueMaps, vars, vars_out) {
      log.Printf("handleTimerTick - trigger '%s' transformations failed", t.ID)
      return
    }
  } else {
    vars_out = vars
  }

  e.HandleTriggerEvent(NewRunID(), t.ID, vars_out)
}

func (e *Engine) StopTimerTriggers() {
  if e.timer_cancels == nil {
    return
  }
  for id, cancel := range e.timer_cancels {
    log.Printf("Stopping timer '%s'...", id)
    cancel()
  }
  e.timer_cancels = nil
}

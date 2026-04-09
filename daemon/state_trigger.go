package main

import (
  "context"
  "log"
)

type StateUpdate struct {
  Key   string
  Value interface{}
}

func (e *Engine) StartStateTriggers() {
  var stateTriggers []DataSource
  for _, t := range e.dataSources {
    if t.Enabled && t.Trigger && t.Type == "state" {
      stateTriggers = append(stateTriggers, t)
    }
  }

  if len(stateTriggers) == 0 {
    return
  }

  if e.state_trigger_cancels == nil {
    e.state_trigger_cancels = make(map[string]context.CancelFunc)
  }

  e.stateTriggerChan = make(chan StateUpdate, 100)

  ctx, cancel := context.WithCancel(context.Background())
  e.state_trigger_cancels["main"] = cancel

  log.Printf("StartStateTriggers - tracking %d state triggers", len(stateTriggers))

  go e.runStateTriggers(ctx, stateTriggers)
}

func (e *Engine) runStateTriggers(ctx context.Context, triggers []DataSource) {
  for {
    select {
    case <-ctx.Done():
      log.Println("Shutting down state triggers...")
      return
    case update := <-e.stateTriggerChan:
      for _, t := range triggers {
        vars := map[string]interface{}{
          "state_key":   update.Key,
          "state_value": update.Value,
        }

        if !matchFilters(t.Filters, vars) {
          continue
        }

        vars_out := make(map[string]interface{})
        if len(t.Transformations) > 0 {
          if !applyTransformations(t.Transformations, e.ValueMaps, vars, vars_out) {
            continue
          }
        } else {
          vars_out = vars
        }

        log.Printf("runStateTriggers - firing trigger '%s' for state '%s'", t.ID, update.Key)
        go e.HandleTriggerEvent(NewRunID(), t.ID, vars_out)
      }
    }
  }
}

func (e *Engine) StopStateTriggers() {
  if e.state_trigger_cancels != nil {
    for _, cancel := range e.state_trigger_cancels {
      cancel()
    }
    e.state_trigger_cancels = nil
  }
  if e.stateTriggerChan != nil {
    close(e.stateTriggerChan)
    e.stateTriggerChan = nil
  }
}

package main

import (
	"context"
	"log"
	"time"
)

func (e *Engine) StartPingTriggers() {
	if e.ping_trigger_cancels == nil {
		e.ping_trigger_cancels = make(map[string]context.CancelFunc)
	}

	for _, ds := range e.dataSources {
		if !ds.Enabled || !ds.Trigger || ds.Type != "ping" {
			continue
		}

		log.Printf("StartPingTriggers - launching loop for: %s", ds.ID)
		ctx, cancel := context.WithCancel(context.Background())
		e.ping_trigger_cancels[ds.ID] = cancel
		go e.startPingTriggerLoop(ctx, ds)
	}
}

func (e *Engine) startPingTriggerLoop(ctx context.Context, ds DataSource) {
	interval := 30 * time.Second
	if ds.Interval != "" {
		if d, err := time.ParseDuration(ds.Interval); err == nil {
			interval = d
		}
	}

	lastState := "unknown"

	for {
		select {
		case <-ctx.Done():
			log.Printf("startPingTriggerLoop - shutting down: %s", ds.ID)
			return
		default:
		}

		runID := NewRunID()
		params := e.GetStates()
		raw, err := e.fetchPing(runID, ds, params)

		var currentState string
		if err != nil {
			log.Printf("[%s] startPingTriggerLoop - '%s' error: %v", runID, ds.ID, err)
			currentState = "down"
		} else if reachable, ok := raw["reachable"].(bool); ok && reachable {
			currentState = "up"
		} else {
			currentState = "down"
		}

		if currentState != lastState {
			vars := map[string]interface{}{
				"current_state":  currentState,
				"previous_state": lastState,
			}
			for k, v := range raw {
				vars[k] = v
			}

			previousState := lastState
			lastState = currentState

			log.Printf("[%s] startPingTriggerLoop - '%s' state change: %s -> %s", runID, ds.ID, previousState, currentState)

			if len(ds.Transformations) > 0 {
				vars_out := make(map[string]interface{})
				if applyTransformations(ds.Transformations, e.ValueMaps, vars, vars_out) {
					e.HandleTriggerEvent(runID, ds.ID, vars_out)
				} else {
					log.Printf("[%s] startPingTriggerLoop - '%s' transformations failed, skipping", runID, ds.ID)
				}
			} else {
				e.HandleTriggerEvent(runID, ds.ID, vars)
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (e *Engine) StopPingTriggers() {
	if e.ping_trigger_cancels == nil {
		return
	}
	for id, cancel := range e.ping_trigger_cancels {
		log.Printf("StopPingTriggers - stopping '%s'", id)
		cancel()
	}
	e.ping_trigger_cancels = nil
}

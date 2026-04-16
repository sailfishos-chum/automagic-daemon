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

    hasSchedule := len(t.ScheduleSeconds) > 0 || len(t.ScheduleMinutes) > 0 || len(t.ScheduleHours) > 0 || len(t.ScheduleDays) > 0 || len(t.ScheduleWeekdays) > 0 || len(t.ScheduleMonths) > 0 || len(t.ScheduleYears) > 0
    log.Printf("StartTimerTriggers - found enabled timer: %s (interval: %s, schedule: %v)", t.ID, t.Interval, hasSchedule)

    ctx, cancel := context.WithCancel(context.Background())
    e.timer_cancels[t.ID] = cancel

    log.Printf("StartTimerTriggers - launching loop for: %s", t.ID)
    go e.startTimerLoop(ctx, t)
  }
}

func (e *Engine) startTimerLoop(ctx context.Context, t DataSource) {
  hasSchedule := len(t.ScheduleSeconds) > 0 || len(t.ScheduleMinutes) > 0 || len(t.ScheduleHours) > 0 || len(t.ScheduleDays) > 0 || len(t.ScheduleWeekdays) > 0 || len(t.ScheduleMonths) > 0 || len(t.ScheduleYears) > 0

  if t.InitialDelay != "" {
    delay, err := time.ParseDuration(t.InitialDelay)
    if err == nil {
      log.Printf("startTimerLoop - timer '%s' waiting for initial_delay: %s", t.ID, t.InitialDelay)
      select {
      case <-ctx.Done():
        return
      case <-time.After(delay):
        now := time.Now()
        if hasSchedule && !matchSchedule(now, t) {
          log.Printf("startTimerLoop - initial_delay finished, but schedule not met for '%s'. Skipping startup tick.", t.ID)
        } else {
          log.Printf("startTimerLoop - initial_delay finished, firing timer '%s' for startup", t.ID)
          e.handleTimerTick(now, t)
        }
      }
    }
  }

  if t.Interval != "" {
    duration, _ := time.ParseDuration(t.Interval)
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
        if hasSchedule && !matchSchedule(tickTime, t) {
          continue
        }
        e.handleTimerTick(tickTime, t)
      }
    }
  } else if hasSchedule {
    log.Printf("startTimerLoop - listening for schedule on timer: %s", t.ID)
    for {
      now := time.Now()
      next := getNextBoundary(now, t)
      sleepDuration := time.Until(next)

      select {
      case <-ctx.Done():
        log.Printf("startTimerLoop - shutting down timer: %s", t.ID)
        return
      case tickTime := <-time.After(sleepDuration):
        if matchSchedule(tickTime, t) {
          e.handleTimerTick(tickTime, t)
        }
      }
    }
  } else {
    log.Printf("startTimerLoop - timer '%s' has no interval and no schedule configured. Exiting loop.", t.ID)
    return
  }
}

func (e *Engine) handleTimerTick(tickTime time.Time, t DataSource) {
  vars := make(map[string]interface{})
  vars_out := make(map[string]interface{})

  vars["trigger_time"] = tickTime.Unix()
  vars["trigger_source"] = t.ID
  vars["epoch"] = tickTime.Unix()
  vars["hour"] = tickTime.Hour()
  vars["minute"] = tickTime.Minute()
  vars["second"] = tickTime.Second()
  vars["date"] = tickTime.Format("2006-01-02")
  vars["time"] = tickTime.Format("15:04:05")
  vars["weekday"] = int(tickTime.Weekday())
  vars["weekday_name"] = tickTime.Weekday().String()
  vars["day"] = tickTime.Day()
  vars["month"] = int(tickTime.Month())
  vars["month_name"] = tickTime.Month().String()
  vars["year"] = tickTime.Year()

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

func getNextBoundary(now time.Time, ds DataSource) time.Time {
  if len(ds.ScheduleSeconds) > 0 {
    return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second()+1, 0, now.Location())
  }
  if len(ds.ScheduleMinutes) > 0 {
    return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute()+1, 0, 0, now.Location())
  }
  if len(ds.ScheduleHours) > 0 {
    return time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location())
  }
  
  if len(ds.ScheduleDays) > 0 || len(ds.ScheduleWeekdays) > 0 || len(ds.ScheduleMonths) > 0 || len(ds.ScheduleYears) > 0 {
    return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
  }
  
  return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute()+1, 0, 0, now.Location())
}

func matchSchedule(tick time.Time, ds DataSource) bool {
  if !intInSlice(tick.Second(), ds.ScheduleSeconds) { return false }
  if !intInSlice(tick.Minute(), ds.ScheduleMinutes) { return false }
  if !intInSlice(tick.Hour(), ds.ScheduleHours) { return false }
  if !intInSlice(tick.Day(), ds.ScheduleDays) { return false }
  if !intInSlice(int(tick.Weekday()), ds.ScheduleWeekdays) { return false }
  if !intInSlice(int(tick.Month()), ds.ScheduleMonths) { return false }
  if !intInSlice(tick.Year(), ds.ScheduleYears) { return false } // The new check
  return true
}

func intInSlice(val int, slice []int) bool {
  if len(slice) == 0 {
    return true
  }
  for _, v := range slice {
    if v == val {
      return true
    }
  }
  return false
}

package main

import (
  "fmt"
  "log"
  "strconv"
  "time"
  "os"
  "math"
  "github.com/nathan-osman/go-sunrise"
)

func (f *Flow) Execute(runID string, e *Engine, initialVars map[string]interface{}) {
  ctxVars := make(map[string]interface{})
  for k, v := range initialVars {
    ctxVars[k] = v
  }
  ctxVars["_run_id"] = runID

  stepMap := make(map[string]int)
  for i, s := range f.Steps {
    if s.ID != "" {
      stepMap[s.ID] = i
    }
  }

  step_visit_counts := make(map[int]int)
  max_step_repeats := 128
  if msr, ok := initialVars["_max_step_repeats"].(int); ok {
    max_step_repeats = msr
  }

  log.Printf("[%s] Execute - Flow %s started", runID, f.ID)

  for i := 0; i < len(f.Steps); i++ {
    step_visit_counts[i]++
    if step_visit_counts[i] > max_step_repeats {
      log.Printf("[%s] ABORTED - Flow %s: Step %d exceeded max repeats (%d)", runID, f.ID, i, max_step_repeats)
      return
    }

    step := f.Steps[i]
    var nextTarget string
    var err error

    conditionPassed := true
    if len(step.If) > 0 {
      conditionPassed = e.evaluateConditions(ctxVars, step.If)
    }

    if !conditionPassed {
      if step.Type == "branch" {
        nextTarget = step.GotoAlt
        log.Printf("[%s] branch - continuing with goto_alt: %s", runID, nextTarget)
      } else {
        continue
      }
    } else {
      switch step.Type {
      case "get":
        nextTarget, err = e.processGet(runID, ctxVars, step)
      case "action":
        nextTarget, err = e.processAction(runID, ctxVars, step, f.ID)
      case "throttle":
        nextTarget, err = e.processThrottle(runID, ctxVars, step, f.ID)
      case "wait":
        nextTarget, err = e.processWait(runID, step)
      case "math":
        nextTarget, err = e.processMath(runID, ctxVars, step)
      case "round":
        nextTarget, err = e.processRound(runID, ctxVars, step)
      case "branch":
        nextTarget = step.Goto
      }
    }

    if err != nil {
      log.Printf("[%s] Error in step %d (%s): %v", runID, i, step.Type, err)
      if nextTarget != "" {
        log.Printf("[%s] Recovering via goto_alt: %s", runID, nextTarget)
      }
    }

    if nextTarget != "" {
      if nextTarget == "end" {
        log.Printf("[%s] Execute - Flow %s finished on end target after step: %d", runID, f.ID, i)
        return
      }
      if nextIdx, exists := stepMap[nextTarget]; exists {
        i = nextIdx - 1
        continue
      }
    }
  }

  log.Printf("[%s] Execute - Flow %s finished", runID, f.ID)
}

func (e *Engine) compareValues(actual interface{}, target interface{}, op string) bool {
  a, errA := e.toFloat(actual)
  t, errT := e.toFloat(target)
  if errA != nil || errT != nil {
    return false
  }

  switch op {
  case ">":  return a > t
  case "<":  return a < t
  case ">=": return a >= t
  case "<=": return a <= t
  }
  return false
}

func (e *Engine) toFloat(v interface{}) (float64, error) {
  switch i := v.(type) {
  case float64: return i, nil
  case float32: return float64(i), nil
  case int:     return float64(i), nil
  case int64:   return float64(i), nil
  case string:  return strconv.ParseFloat(i, 64)
  default:      return 0, fmt.Errorf("not a number")
  }
}

func (e *Engine) resolveParams(params map[string]interface{}, vars map[string]interface{}) map[string]interface{} {
  resolved := make(map[string]interface{})
  for k, v := range params {
    if str_val, ok := v.(string); ok {
      resolved[k] = expandByTemplate(str_val, vars)
    } else {
      resolved[k] = v
    }
  }
  return resolved
}

func (e *Engine) callInternalFunction(fn string, params map[string]interface{}) (map[string]interface{}, error) {
  res := make(map[string]interface{})
  now := time.Now()

  switch fn {
  case "time":
    res["epoch"] = now.Unix()
    res["hour"] = now.Hour()
    res["minute"] = now.Minute()
    res["second"] = now.Second()
    res["date"] = now.Format("2006-01-02")
    res["time"] = now.Format("15:04:05")
    res["weekday"] = int(now.Weekday())
    res["weekday_name"] = now.Weekday().String()

  case "device":
    host, _ := os.Hostname()
    res["hostname"] = host

  case "random":
    res["value"] = now.UnixNano() % 100

  case "state":
    if name, ok := params["name"].(string); ok {
      res["state"], _ = e.GetState(name)
    }

  case "darkness":
    var lat, lon float64

    if v, ok := params["latitude"]; ok {
      switch val := v.(type) {
      case float64:
        lat = val
      case int:
        lat = float64(val)
      case string:
        lat, _ = strconv.ParseFloat(val, 64)
      }
    }

    if v, ok := params["longitude"]; ok {
      switch val := v.(type) {
      case float64:
        lon = val
      case int:
        lon = float64(val)
      case string:
        lon, _ = strconv.ParseFloat(val, 64)
      }
    }

    rise, set := sunrise.SunriseSunset(lat, lon, now.Year(), now.Month(), now.Day())

    rise = rise.In(now.Location())
    set = set.In(now.Location())

    res["is_dark"] = now.Before(rise) || now.After(set)
    res["sunrise"] = rise.Format("15:04:05")
    res["sunset"] = set.Format("15:04:05")
    res["sunrise_hour"] = rise.Hour()
    res["sunrise_minute"] = rise.Minute()
    res["sunset_hour"] = set.Hour()
    res["sunset_minute"] = set.Minute()

  default:
    return nil, fmt.Errorf("unknown internal function: %s", fn)
  }

  log.Printf("callInternalFunction - res: %v", res)
  return res, nil
}

func (e *Engine) processGet(runID string, ctxVars map[string]interface{}, step Step) (string, error) {
  var data map[string]interface{}
  var err error

  if step.Function != "" {
    data, err = e.callInternalFunction(step.Function, step.Params)
  } else if step.Source != "" {
    data, err = e.FetchData(runID, step.Source)
  } else {
    return step.GotoAlt, fmt.Errorf("get step missing function or source")
  }

  if err != nil {
    log.Printf("[%s] Get error: %v", runID, err)
    return step.GotoAlt, err
  }

  if len(step.Mapping) > 0 {
    for srcKey, targetVar := range step.Mapping {
      if val, ok := data[srcKey]; ok {
        ctxVars[targetVar] = val
        log.Printf("[%s] Get mapped: %s -> %s (%v)", runID, srcKey, targetVar, val)
      }
    }
  } else {
    for k, v := range data {
      ctxVars[k] = v
    }
  }

  return step.Goto, nil
}

func (e *Engine) processAction(runID string, ctxVars map[string]interface{}, step Step, flowID string) (string, error) {
  resolvedParams := e.resolveParams(step.Params, ctxVars)

  if step.Function != "" {
    log.Printf("[%s] Running Internal Function: %s", runID, step.Function)
    
    switch step.Function {
    case "set_state":
      nameRaw, hasName := resolvedParams["name"]
      if !hasName {
        log.Printf("[%s] set_state error: missing 'name' parameter", runID)
        return step.GotoAlt, fmt.Errorf("missing 'name' parameter")
      }
      
      stateName, ok := nameRaw.(string)
      if !ok {
        log.Printf("[%s] set_state error: 'name' must resolve to a string", runID)
        return step.GotoAlt, fmt.Errorf("'name' must resolve to a string")
      }

      var stateVal interface{}
      valueSet := false

      if varName, ok := step.Params["variable"].(string); ok {
        stateVal = ctxVars[varName]
        valueSet = true
      } else if staticVal, ok := step.Params["static"]; ok {
        stateVal = staticVal
        valueSet = true
      } else if tplVal, ok := resolvedParams["template"]; ok {
        stateVal = tplVal
        valueSet = true
      }

      if valueSet {
        e.WriteState(stateName, stateVal)
        log.Printf("[%s] State set (internal): %s -> %v", runID, stateName, stateVal)
      } else {
        log.Printf("[%s] set_state error: missing 'variable', 'template', or 'static'", runID)
        return step.GotoAlt, fmt.Errorf("missing 'variable', 'template', or 'static'")
      }
    default:
      log.Printf("[%s] Unknown internal function: %s", runID, step.Function)
      return step.GotoAlt, fmt.Errorf("Unknown internal function: %s", step.Function)
    }
    
    return step.Goto, nil
  }

  log.Printf("[%s] Running Action: %s", runID, step.Action)

  err := e.RunAction(runID, step.Action, resolvedParams, step.ID, flowID)
  if err != nil {
    return step.GotoAlt, err
  }

  return step.Goto, nil
}

func (e *Engine) processThrottle(runID string, ctxVars map[string]interface{}, step Step, flowID string) (string, error) {

  lockKey := flowID + "_" + step.ID
  if scope, ok := step.Params["scope"].([]interface{}); ok {
    for _, s := range scope {
      keyName := fmt.Sprintf("%v", s)
      val := ctxVars[keyName]
      lockKey += fmt.Sprintf("_%v", val)
    }
  }

  e.throttleMu.Lock()
  defer e.throttleMu.Unlock()

  lastRun, exists := e.lastThrottleRuns[lockKey]
  
  durationStr, ok := step.Params["duration"].(string)
  if !ok {
    return "", fmt.Errorf("wait error: duration missing or not a string")
  }

  duration, err := time.ParseDuration(durationStr)
  if err != nil {
    return "", fmt.Errorf("wait error: invalid duration '%s': %v", durationStr, err)
  }

  isThrottled := exists && time.Since(lastRun) < duration

  if isThrottled {
    log.Printf("[%s] Throttle ACTIVE for %s. Executing goto: %s", runID, lockKey, step.GotoAlt)
    return step.GotoAlt, nil
  }

  e.lastThrottleRuns[lockKey] = time.Now()
  
  return step.Goto, nil
}

func (e *Engine) processWait(runID string, step Step) (string, error) {
  durationStr, ok := step.Params["duration"].(string)
  if !ok {
    return step.GotoAlt, fmt.Errorf("wait error: duration missing or not a string")
  }

  duration, err := time.ParseDuration(durationStr)
  if err != nil {
    return step.GotoAlt, fmt.Errorf("wait error: invalid duration '%s': %v", durationStr, err)
  }

  log.Printf("[%s] Waiting %v...", runID, duration)
  time.Sleep(duration)
  
  return step.Goto, nil
}

func (e *Engine) processMath(runID string, ctxVars map[string]interface{}, step Step) (string, error) {
  inRaw, hasIn := step.Params["in"]
  outRaw, hasOut := step.Params["out"]

  if !hasIn || !hasOut {
    err := fmt.Errorf("math step requires 'in' and 'out' in params")
    log.Printf("[%s] %v", runID, err)
    return step.GotoAlt, err
  }

  inExpr, inOk := inRaw.(string)
  outVar, outOk := outRaw.(string)

  if !inOk || !outOk {
    err := fmt.Errorf("math 'in' and 'out' params must be strings")
    log.Printf("[%s] %v", runID, err)
    return step.GotoAlt, err
  }

  expr := expandByTemplate(inExpr, ctxVars, ctxVars)
  
  if inExpr == expr || expr == "" {
    err := fmt.Errorf("math variables not found or resolved to empty: %s", inExpr)
    log.Printf("[%s] %v", runID, err)
    return step.GotoAlt, err
  }

  valueOut, err := evalMath(expr)
  if err != nil {
    log.Printf("[%s] Math expression error: %s", runID, err)
    return step.GotoAlt, err
  }

  ctxVars[outVar] = valueOut
  log.Printf("[%s] Math evaluated: %s -> %s = %v", runID, inExpr, outVar, valueOut)

  return step.Goto, nil
}

func (e *Engine) processRound(runID string, ctxVars map[string]interface{}, step Step) (string, error) {
  inKey, _ := step.Params["in"].(string)
  outVar, _ := step.Params["out"].(string)
  
  if inKey == "" || outVar == "" {
    return step.GotoAlt, fmt.Errorf("round requires 'in' and 'out' strings")
  }

  val := ctxVars[inKey]
  
  f, ok := toFloat64(val)
  if !ok {
    log.Printf("[%s] Round error: %v is not a number", runID, val)
    return step.GotoAlt, fmt.Errorf("value not a float")
  }

  decimals := 0
  if d, ok := step.Params["decimals"].(float64); ok {
    decimals = int(d)
  }

  p := math.Pow(10, float64(decimals))
  rounded := math.Round(f*p) / p
  
  ctxVars[outVar] = rounded
  log.Printf("[%s] Round: %v -> %v (%d decimals)", runID, f, rounded, decimals)

  return step.Goto, nil
}

func (e *Engine) evaluateConditions(ctxVars map[string]interface{}, conds []Condition) bool {
  if len(conds) == 0 {
    return true
  }


  result := e.evaluateSingle(ctxVars, conds[0])

  for i := 1; i < len(conds); i++ {
    nextResult := e.evaluateSingle(ctxVars, conds[i])
    
    if conds[i].Logic == "or" {
      result = result || nextResult
    } else {
      result = result && nextResult
    }
  }

  return result
}

func (e *Engine) evaluateSingle(ctxVars map[string]interface{}, cond Condition) bool {
  leftVal := e.resolveSide(cond.LeftVar, cond.LeftState, cond.LeftConst, ctxVars)
  
  if cond.Op == "exists" {
    return leftVal != nil
  }

  rightVal := e.resolveSide(cond.RightVar, cond.RightState, cond.RightConst, ctxVars)

  switch cond.Op {
  case "==":
    return leftVal == rightVal
  case "!=":
    return leftVal != rightVal
  case ">", "<", ">=", "<=":
    return e.compareValues(leftVal, rightVal, cond.Op)
  }

  return false
}

func (e *Engine) resolveSide(varName, stateName string, constVal interface{}, ctxVars map[string]interface{}) interface{} {
  if varName != "" {
    return ctxVars[varName]
  }
  if stateName != "" {
    val, _ := e.GetState(stateName)
    return val
  }
  return constVal
}

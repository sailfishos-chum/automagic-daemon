package main

import (
	"log"
  "time"
  "fmt"
  "strconv"
  "os"
  "os/user"
  "path/filepath"
  "encoding/json"
  "github.com/godbus/dbus/v5"
)

func NewEngine() *Engine {
	e := &Engine{
		dataSources:        make(map[string]DataSource),
		flows:              []Flow{},
		actions:            make(map[string]Action),
		States:             make(map[string]interface{}),
    Data:               make(map[string]map[string]interface{}),
    lastThrottleRuns:   make(map[string]time.Time),
    broadcastChan:      make(chan interface{}, 100),
	}

	return e
}

func (e *Engine) loadVersioned(path string, target interface{}) error {
  content, err := os.ReadFile(path)
  if err != nil {
    return err
  }

  var env struct {
    Version int             `json:"version"`
    Data    json.RawMessage `json:"data"`
  }

  if err := json.Unmarshal(content, &env); err != nil {
    return err
  }

  if env.Version != 1 {
    return fmt.Errorf("config version %d not supported in %s", env.Version, path)
  }

  return json.Unmarshal(env.Data, target)
}

func (e *Engine) HandleTriggerEvent(runID string, triggerID string, vars map[string]interface{}) {
  log.Printf("[%s] HandleTriggerEvent - started for trigger: %s", runID, triggerID)

  for _, flow := range e.flows {
    if !flow.Enabled {
      continue
    }

    matched := false
    for _, tid := range flow.Triggers {
      if tid == triggerID {
        matched = true
        break
      }
    }
    
    if !matched {
      continue
    }

    log.Printf("[%s] HandleTriggerEvent - Executing flow %s for trigger %s", runID, flow.ID, triggerID)
    
    go flow.Execute(runID, e, vars) 
  }
}

func (e *Engine) WriteState(key string, value interface{}) {
  e.statesMu.Lock()
  e.States[key] = value
  e.statesMu.Unlock()

  if e.broadcastChan != nil {
    msg := map[string]interface{}{
      "type":  "state_update",
      "key":   key,
      "value": value,
    }
    select {
    case e.broadcastChan <- msg:
    default:
      log.Printf("WriteState - Broadcast buffer full, dropping update for %s", key)
    }
  }

  

  if e.stateTriggerChan != nil {
    update := StateUpdate{Key: key, Value: value}
    select {
    case e.stateTriggerChan <- update:
    default:
      log.Printf("WriteState - State trigger buffer full, dropping internal trigger for %s", key)
    }
  }
}

func (e *Engine) GetState(key string) (interface{}, bool) {
  e.statesMu.RLock()
  defer e.statesMu.RUnlock()
  val, ok := e.States[key]
  return val, ok
}

func (e *Engine) GetStates() map[string]interface{} {
  e.statesMu.RLock()
  defer e.statesMu.RUnlock()
  out := make(map[string]interface{}, len(e.States))
  for k, v := range e.States {
    out[k] = v
  }

  return out
}

func (e *Engine) WriteData(name string, fields map[string]interface{}) {
  e.dataMu.Lock()
  defer e.dataMu.Unlock()

  if _, ok := e.Data[name] ; !ok {
    e.Data[name] = make(map[string]interface{})
  }

  for k, v := range fields {
    if k == "_data_record_name" {
      continue
    }
    e.Data[name][k] = v
  }
}

func (e *Engine) GetDataRecord(name string) (map[string]interface{}, bool) {
  e.dataMu.RLock()
  defer e.dataMu.RUnlock()
  val, ok := e.Data[name]
  return val, ok
}

func (e *Engine) GetData() map[string]map[string]interface{} {
  e.dataMu.RLock()
  defer e.dataMu.RUnlock()
  out := make(map[string]map[string]interface{}, len(e.Data))
  for k, v := range e.Data {
    out[k] = v
  }

  return out
}

func (e *Engine) ResolveDBusArg(typeCode string, val interface{}) interface{} {
  sVal, isString := val.(string)
  fVal, isFloat := val.(float64)

  switch typeCode {
  case "s", "string":
    return fmt.Sprintf("%v", val)

  case "u", "uint32":
    if isFloat { return uint32(fVal) }
    if isString {
      v, _ := strconv.ParseUint(sVal, 10, 32)
      return uint32(v)
    }
    return uint32(0)

  case "i", "int", "int32":
    if isFloat { return int32(fVal) }
    if isString {
      v, _ := strconv.ParseInt(sVal, 10, 32)
      return int32(v)
    }
    return int32(0)

  case "b", "bool", "boolean":
    if b, ok := val.(bool); ok { return b }
    if isString { return sVal == "true" || sVal == "1" }
    return false

  case "as":
    if isString {
      var res []string
      json.Unmarshal([]byte(sVal), &res)
      return res
    }
    return []string{}

  case "a{sv}":
    res := make(map[string]dbus.Variant)
    if isString {
      var raw map[string]interface{}
      if err := json.Unmarshal([]byte(sVal), &raw); err == nil {
        for k, v := range raw {
          res[k] = dbus.MakeVariant(v)
        }
      }
    }
    return res

  default:
    return val
  }
}

func (e *Engine) FetchData(runID string, sourceID string) (map[string]interface{}, error) {
  ds, exists := e.dataSources[sourceID]
  if !exists {
    return nil, fmt.Errorf("source %s not found", sourceID)
  }

  var raw map[string]interface{}
  var err error

  switch ds.Type {
  case "dbus":
    raw, err = e.fetchDBus(runID, ds)
  case "file":
    raw, err = e.fetchFile(runID, ds)
  case "http":
    raw, err = e.fetchHTTP(runID, ds)
  case "sqlite":
    raw, err = e.fetchSQL(runID, ds)
  case "mysql":
    raw, err = e.fetchSQL(runID, ds)
  case "imap":
    raw, err = e.fetchIMAP(runID, ds)
  default:
    return nil, fmt.Errorf("unsupported fetch type: %s", ds.Type)
  }

  if err != nil {
    return nil, err
  }

  cleanVars := make(map[string]interface{})
  applyTransformations(ds.Transformations, e.ValueMaps, raw, cleanVars)

  return cleanVars, nil
}

func (e *Engine) RunAction(runID string, actionID string, flowParams map[string]interface{}, stepID string, flowID string) error {
  var action_o Action

  action_def, ok := e.actions[actionID]
  if ok {
    action_o = action_def
  } else {
    action_o = Action{
      ID:   actionID,
      Type: actionID,
    }
  }

  fn, exists := actionRegistry[action_o.Type]
  if !exists {
    log.Printf("[%s] RunAction - No function registered for type: %s", runID, action_o.Type)
    return nil
  }

  return fn(runID, e, action_o, flowParams, stepID, flowID)
}

func (e *Engine) LoadSecret() error {
  var secret_data struct { SharedSecret string `json:"shared_secret"` }

  if err := e.loadVersioned(e.ConfigPath+"/secret.json", &secret_data); err != nil {
    log.Printf("LoadSecret - error loading %s: %s", e.ConfigPath+"/secret.json", err)
    return err
  }
  e.SharedSecret = secret_data.SharedSecret

  return nil
}

func (e *Engine) LoadConfig(configPath string) error {
  var data_sources []DataSource
  var actions []Action
  var flows []Flow 
  var value_maps map[string]map[string]interface{} 

  if err := e.loadVersioned(configPath+"/data_sources.json", &data_sources); err != nil {
    log.Printf("LoadConfig - error loading %s: %s", configPath+"/data_sources.json", err)
    return err
  }

  e.dataSources = make(map[string]DataSource)
  for _, t := range data_sources {
    e.dataSources[t.ID] = t
  }

  if err := e.loadVersioned(configPath+"/actions.json", &actions); err != nil {
    log.Printf("LoadConfig - error loading %s: %s", configPath+"/actions.json", err)
    return err
  }

  e.actions = make(map[string]Action)
  for _, a := range actions {
    e.actions[a.ID] = a
  }

  if err := e.loadVersioned(configPath+"/flows.json", &flows); err != nil {
    log.Printf("LoadConfig - error loading %s: %s", configPath+"/flows.json", err)
    return err
  }
  e.flows = flows

  if err := e.loadVersioned(configPath+"/value_maps.json", &value_maps); err != nil {
    log.Printf("LoadConfig - error loading %s: %s", configPath+"/value_maps.json", err)
    return err
  }
  e.ValueMaps = value_maps

  return nil
}

func (e *Engine) SetUser(config_path string) error {
  u, err := user.Current()
  if err == nil {
    e.ConfigPath = config_path
    e.SessionUser = u
    e.UID = u.Uid
    e.Username = u.Username
    return nil
  }

  return fmt.Errorf("could not find a valid user configuration")
}


func (e *Engine) DiscoverUser() error {
  files, _ := os.ReadDir("/home")
  for _, f := range files {
    if f.IsDir() {
      username := f.Name()
      basePath := filepath.Join("/home", username, ".config", "app.qml", "automagic")
            
      if _, err := os.Stat(basePath); err == nil {
        u, err := user.Lookup(username)
        if err == nil {
          e.ConfigPath = basePath
          e.SessionUser = u
          e.UID = u.Uid
          e.Username = u.Username
          return nil
        }
      }
    }
  }
  return fmt.Errorf("could not find a valid user configuration")
}

func (e *Engine) Start() error {
  if len(e.ConfigPathStatic) > 0 {
    err := e.SetUser(e.ConfigPathStatic)
    if err != nil {
      log.Printf("Start - setting static config path failed: %s with error: %s", e.ConfigPathStatic, err)
      return err
    }
  }

  if err := e.StartUnixSocket(); err != nil {
    return err
  }

  e.StartBroadcaster()
  e.Reload()

  return nil
}

func (e *Engine) Stop() {
  e.StopBroadcaster()
  e.StopTimerTriggers()
  e.StopDBusTriggers()
  e.StopMQTTTriggers()
  e.StopStateTriggers()
  e.StopFileTriggers()
}

func (e *Engine) Reload() error {
  e.StopTimerTriggers()
  e.StopDBusTriggers()
  e.StopMQTTTriggers()
  e.StopStateTriggers()
  e.StopFileTriggers()

  if len(e.ConfigPathStatic) < 1 {
    if e.DiscoverUser() != nil {
      log.Println("Reload - user discovery failed, entering standby mode")
      e.SharedSecret = ""
      return nil
    }
  }

  log.Printf("Reload - starting automagic - config path: %s, user: %s, uid: %s", e.ConfigPath, e.Username, e.UID)

  if e.SessionUser == nil || e.ConfigPath == "" {
    log.Println("Reload - No configuration found, entering standby mode")
    e.SharedSecret = ""
    return nil
  }

  if err := e.LoadSecret(); err != nil {
    log.Printf("Reload - error loading shared secret: %v", err)
  }

  if err := e.LoadConfig(e.ConfigPath); err != nil {
    log.Printf("Reload - config missing or invalid: %v", err)
  }

  e.StartFileTriggers()
  e.StartMQTTTriggers()
  e.StartDBusTriggers()
  e.StartTimerTriggers()
  e.StartStateTriggers()

  return nil
}

func (e *Engine) ExecuteFlow(runID string, flow_id string, vars map[string]interface{}) error {
  for _, flow := range e.flows {
    
    if flow.ID != flow_id {
      continue
    }

    log.Printf("[%s] ExecuteFlow - Executing flow %s from API", runID, flow.ID)
    go flow.Execute(runID, e, vars) 
  }

  return nil
}


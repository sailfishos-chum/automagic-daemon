package main

import (
  "sync"
  "context"
  "time"
  "net"
  "os/user"
)

type Engine struct {
  ConfigPath string
  ConfigPathStatic string
  SessionUser *user.User
  UID        string
  Username   string
  broker_cancels map[string]context.CancelFunc
  dbus_cancels map[string]context.CancelFunc
  timer_cancels map[string]context.CancelFunc
  file_watcher_cancels map[string]context.CancelFunc
  stateTriggerCancel map[string]context.CancelFunc
  dataSources map[string]DataSource
  mqtt_brokers map[string]*MQTTBroker
  file_watchers map[string]*FileWatcher
	flows    []Flow
	actions  map[string]Action
	States   map[string]interface{}
  statesMu sync.RWMutex
  Data     map[string]map[string]interface{}
  dataMu   sync.RWMutex
  listeners   map[net.Conn]bool
  listenerMu  sync.Mutex
  broadcastChan chan interface{}
  stateTriggerChan chan StateUpdate
  ValueMaps map[string]map[string]interface{}
  lastThrottleRuns map[string]time.Time
  throttleMu       sync.Mutex
  
  SharedSecret string
}

type MQTTBroker struct {
  Address  string
  Username string
  Password string
  Triggers []DataSource
}

type FileWatcher struct {
  File string
  Triggers []DataSource
}

type Transformation struct {
  Type          string                 `json:"type,omitempty"`
  In            string                 `json:"in,omitempty"`
  Out           string                 `json:"out,omitempty"`
  Map           string                 `json:"map,omitempty"`
  Optional      bool                   `json:"optional,omitempty"`
  DecimalPlaces int                    `json:"decimal_places,omitempty"`
}

type DataSource struct {
  ID              string                 `json:"id"`
  Name            string                 `json:"name,omitempty"`
  Type            string                 `json:"type"`
  Enabled         bool                   `json:"enabled"`
  Address         string                 `json:"address,omitempty"`
  Username        string                 `json:"username,omitempty"`
  Password        string                 `json:"password,omitempty"`
  Topic           string                 `json:"topic,omitempty"`
  Interface       string                 `json:"interface,omitempty"`
  Path            string                 `json:"path,omitempty"`
  Database        string                 `json:"database,omitempty"`
  Query           string                 `json:"query,omitempty"`
  Destination     string                 `json:"destination,omitempty"`
  Method          string                 `json:"method,omitempty"`
  Signal          string                 `json:"signal,omitempty"`
  Args            []TypedArg             `json:"args,omitempty"`
  Format          string                 `json:"format,omitempty"`
  Pattern         string                 `json:"pattern,omitempty"`
  Delimiter       string                 `json:"delimiter,omitempty"`
  Trigger         bool                   `json:"trigger,omitempty"`
  Filters         map[string]interface{} `json:"filters,omitempty"`
  Transformations []Transformation       `json:"transformations,omitempty"`
  Interval        string                 `json:"interval,omitempty"`
  InitialDelay    string                 `json:"initial_delay,omitempty"`
  Insecure        bool                   `json:"insecure,omitempty"`
}

type TypedArg struct {
  Type  string      `json:"type"`
  Value interface{} `json:"value"`
}

type Flow struct {
  ID        string                 `json:"id"`
  Name      string                 `json:"name,omitempty"`
  Enabled   bool                   `json:"enabled"`
  Triggers  []string               `json:"triggers,omitempty"`
  Actions   []string               `json:"actions,omitempty"`
  Filters   map[string]interface{} `json:"filters,omitempty"`
  Steps     []Step                 `json:"steps,omitempty"`
}

type Step struct {
  ID        string                 `json:"id,omitempty"`
  Type      string                 `json:"type"`
  Source    string                 `json:"source,omitempty"`
  Function  string                 `json:"function,omitempty"`
  Mapping   map[string]string      `json:"mapping,omitempty"`
  Action    string                 `json:"action,omitempty"`
  Params    map[string]interface{} `json:"params,omitempty"`
  Goto      string                 `json:"goto,omitempty"`
  GotoAlt   string                 `json:"goto_alt,omitempty"`
  Name      string                 `json:"name,omitempty"`
  If        []Condition            `json:"if,omitempty"`
}

type Condition struct {
  Logic      string      `json:"logic,omitempty"`
  Op         string      `json:"op"`
  LeftVar    string      `json:"left_var,omitempty"`
  LeftState  string      `json:"left_state,omitempty"`
  LeftConst  interface{} `json:"left_const,omitempty"`
  RightVar   string      `json:"right_var,omitempty"`
  RightState string      `json:"right_state,omitempty"`
  RightConst interface{} `json:"right_const,omitempty"`
}

type Action struct {
  ID          string                 `json:"id"`
  Name        string                 `json:"name,omitempty"`
  Type        string                 `json:"type"`
  Enabled     bool                   `json:"enabled"`
  Address     string                 `json:"address,omitempty"`
  ContentType string                 `json:"content_type,omitempty"`
  Payload     string                 `json:"payload,omitempty"`
  Destination string                 `json:"destination,omitempty"`
  Path        string                 `json:"path,omitempty"`
  Database    string                 `json:"database,omitempty"`
  Query       string                 `json:"query,omitempty"`
  Interface   string                 `json:"interface,omitempty"`
  Method      string                 `json:"method,omitempty"`
  Args        []interface{}          `json:"args,omitempty"`
  TimeoutMs   int                    `json:"timeout_ms,omitempty"`
  Username    string                 `json:"username,omitempty"`
  Password    string                 `json:"password,omitempty"`
  Topic       string                 `json:"topic,omitempty"`
  Qos         byte                   `json:"qos,omitempty"`
  Retained    bool                   `json:"retained,omitempty"`
  Command     string                 `json:"command,omitempty"`
  Message     string                 `json:"message,omitempty"`

  Host        string                 `json:"host,omitempty"`
  Port        string                 `json:"port,omitempty"`
  From        string                 `json:"from,omitempty"`
  To          string                 `json:"to,omitempty"`
  Bcc         string                 `json:"bcc,omitempty"`
  Subject     string                 `json:"subject,omitempty"`
  Body        string                 `json:"body,omitempty"`
  Insecure        bool                   `json:"insecure,omitempty"`
}

type SocketRequest struct {
  Secret  string                 `json:"secret"`
  Cmd     string                 `json:"cmd"`
  Trigger string                 `json:"trigger,omitempty"`
  Flow    string                 `json:"flow,omitempty"`
  Vars    map[string]interface{} `json:"vars,omitempty"`
}

type SocketResponse struct {
  Ok     bool                               `json:"ok"`
  Error  string                             `json:"error,omitempty"`
  States map[string]interface{}             `json:"states,omitempty"`
  Data   map[string]map[string]interface{}  `json:"data,omitempty"`
}

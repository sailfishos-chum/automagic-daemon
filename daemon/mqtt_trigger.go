package main

import (
  "context"
  "encoding/json"
  "math/rand"
  "fmt"
  "log"
  "time"
  "crypto/sha256"
  "encoding/hex"
  mqtt "github.com/eclipse/paho.mqtt.golang"
)

func (e *Engine) StartMQTTTriggers() {
  if e.mqtt_brokers == nil {
    e.mqtt_brokers = make(map[string]*MQTTBroker)
  }

  if e.broker_cancels == nil {
    e.broker_cancels = make(map[string]context.CancelFunc)
  }

  for _, t := range e.dataSources {
    if !t.Enabled || !t.Trigger || t.Type != "mqtt" {
      continue
    }

    hash := sha256.Sum256([]byte(t.Password))
    pass_hash := hex.EncodeToString(hash[:8])
    broker_id := fmt.Sprintf("%s|%s|%s", t.Address, t.Username, pass_hash)

    if _, ok := e.mqtt_brokers[broker_id]; !ok {
      broker := &MQTTBroker{
        Address:   t.Address,
        Username: t.Username,
        Password: t.Password,
      }
      e.mqtt_brokers[broker_id] = broker
    }

    e.mqtt_brokers[broker_id].Triggers = append(e.mqtt_brokers[broker_id].Triggers, t)
  }

  for brokerID, b := range e.mqtt_brokers {
    log.Printf("StartMQTTTriggers - broker: %v, triggers: %d", b.Address, len(b.Triggers))
    
    ctx, cancel := context.WithCancel(context.Background())
    e.broker_cancels[brokerID] = cancel

    go e.startMQTTBroker(ctx, b)
  }
}

func (e *Engine) startMQTTBroker(ctx context.Context, b *MQTTBroker) {
  log.Printf("startMQTTBroker - broker: %s, user: %s", b.Address, b.Username)

  client_id := fmt.Sprintf("automagic-%d", rand.Int())

  opts := mqtt.NewClientOptions()
  opts.AddBroker("tcp://" + b.Address)
  opts.SetClientID("automagic-" + client_id)
  opts.SetAutoReconnect(true)
  opts.SetConnectRetry(true)
  opts.SetConnectRetryInterval(5 * time.Second)

  if b.Username != "" {
    opts.SetUsername(b.Username)
    opts.SetPassword(b.Password)
  }

  topics := make(map[string]bool)
  for _, t := range b.Triggers {
    if t.Topic != "" {
      topics[t.Topic] = true
    }
  }

  opts.OnConnect = func(c mqtt.Client) {
    log.Printf("startMQTTBroker - broker '%s' connected", b.Address)

    for topic := range topics {
      if token := c.Subscribe(topic, 0, e.makeMQTTHandler(b)); token.Wait() && token.Error() != nil {
        log.Printf("startMQTTBroker - broker '%s' subscribe error: %v", b.Address, token.Error())
      } else {
        log.Printf("startMQTTBroker - broker '%s' subscribed to %s", b.Address, topic)
      }
    }
  }

  opts.OnConnectionLost = func(_ mqtt.Client, err error) {
    log.Printf("startMQTTBroker - trigger '%s' connection lost: %v", b.Address, err)
  }

  client := mqtt.NewClient(opts)

  if token := client.Connect(); token.Wait() && token.Error() != nil {
    log.Printf("startMQTTBroker - trigger '%s' connect error: %v", b.Address, token.Error())
    return
  }

  <-ctx.Done()

  log.Printf("startMQTTBroker - shutting down connection to broker '%s'", b.Address)
  client.Disconnect(250)
}

func (e *Engine) makeMQTTHandler(b *MQTTBroker) mqtt.MessageHandler {
  return func(_ mqtt.Client, msg mqtt.Message) {
    for _, t := range b.Triggers {
      if !topicMatches(t.Topic, msg.Topic()) {
        continue
      }

      runID := NewRunID()

      vars := make(map[string]interface{})
      vars_out := make(map[string]interface{})

      switch t.Format {
      case "", "json":
        if err := json.Unmarshal(msg.Payload(), &vars); err != nil {
          log.Printf("makeMQTTHandler - trigger '%s' JSON decode error: %v", t.ID, err)
          continue
        }
      default:
        log.Printf("makeMQTTHandler - trigger '%s' unsupported format '%s'", t.ID, t.Format)
        continue
      }

      vars["_topic"] = msg.Topic()

      if !matchFilters(t.Filters, vars) {
        continue
      }

      log.Printf("makeMQTTHandler - trigger '%s' event from %s", t.ID, msg.Topic())

      if len(t.Transformations) > 0 {
        if !applyTransformations(t.Transformations, e.ValueMaps, vars, vars_out) {
          log.Printf("makeMQTTHandler - trigger '%s' transformations failed - ending execution.", t.ID)
          continue
        }
      }

      log.Printf("makeMQTTHandler - trigger '%s' output variables: %v", t.ID, vars_out)

      e.HandleTriggerEvent(runID, t.ID, vars_out)
    }
  }
}

func (e *Engine) StopMQTTTriggers() {
  if e.broker_cancels == nil {
    return
  }
  
  for brokerID, cancel := range e.broker_cancels {
    log.Printf("Signaling broker %s to shut down...", brokerID)
    cancel()
  }

  e.broker_cancels = nil
  e.mqtt_brokers = nil
}

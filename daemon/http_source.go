package main

import (
  "fmt"
  "io"
  "net/http"
  "time"
  "log"
  "strings"
  "crypto/tls"
)

func (e *Engine) fetchHTTP(runID string, ds DataSource) (map[string]interface{}, error) {
  if ds.Address == "" {
    return nil, fmt.Errorf("http source '%s' requires an address", ds.ID)
  }

  method := "GET"
  if ds.Method != "" {
    method = strings.ToUpper(ds.Method)
  }

  var bodyReader io.Reader
  if ds.Payload != "" {
    bodyReader = strings.NewReader(ds.Payload)
  }

  req, err := http.NewRequest(method, ds.Address, bodyReader)
  if err != nil {
    return nil, err
  }
  
  req.Header.Set("User-Agent", "automagicd/1.0")

  if ds.ContentType != "" {
    req.Header.Set("Content-Type", ds.ContentType)
  }

  if ds.Username != "" {
    req.SetBasicAuth(ds.Username, ds.Password)
  }

  timeout := 10 * time.Second
  if ds.Timeout != "" {
    if parsed, err := time.ParseDuration(ds.Timeout); err == nil {
      timeout = parsed
    } else {
      log.Printf("[%s] fetchHTTP - invalid timeout '%s', defaulting to 10s: %v", runID, ds.Timeout, err)
    }
  }

  customTransport := http.DefaultTransport.(*http.Transport).Clone()
  if ds.Insecure {
    if customTransport.TLSClientConfig == nil {
      customTransport.TLSClientConfig = &tls.Config{}
    }
    customTransport.TLSClientConfig.InsecureSkipVerify = true
  }

  client := &http.Client{
    Timeout:   timeout,
    Transport: customTransport,
  }

  resp, err := client.Do(req)
  if err != nil {
    return nil, err
  }
  defer resp.Body.Close()

  if resp.StatusCode < 200 || resp.StatusCode >= 300 {
    return nil, fmt.Errorf("http error, status code: %d", resp.StatusCode)
  }

  bodyBytes, err := io.ReadAll(resp.Body)
  if err != nil {
    return nil, err
  }

  data, err := parsePayload(ds.Format, bodyBytes, ds.Pattern, ds.Delimiter)
  if err != nil {
    return nil, fmt.Errorf("parse error from %s: %v", ds.Address, err)
  }

  return data, nil
}

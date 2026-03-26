package main

import (
  "fmt"
  "io"
  "net/http"
  "time"
)

func (e *Engine) fetchHTTP(runID string, ds DataSource) (map[string]interface{}, error) {
  if ds.Address == "" {
    return nil, fmt.Errorf("http source '%s' requires an address", ds.ID)
  }

  req, err := http.NewRequest("GET", ds.Address, nil)
  if err != nil {
    return nil, err
  }
  req.Header.Set("User-Agent", "automagicd/1.0")

  client := &http.Client{Timeout: 10 * time.Second}
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

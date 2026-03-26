package main

import (
    "encoding/json"
    "io/ioutil"
    "log"
    "os"
)

func LoadJSONConfig[T any](path string) (T, error) {
  log.Println("Loading config from", path)

  var config T
  file, err := os.Open(path)
  if err != nil {
      return config, err
  }
  defer file.Close()

  bytes, err := ioutil.ReadAll(file)
  if err != nil {
      return config, err
  }

  err = json.Unmarshal(bytes, &config)
  if err != nil {
      return config, err
  }

  return config, nil
}

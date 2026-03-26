package main

import (
  "encoding/json"
  "fmt"
  "regexp"
  "strings"
)

func parsePayload(format string, rawData []byte, pattern string, delimiter string) (map[string]interface{}, error) {
  data := make(map[string]interface{})

  if format == "" {
    format = "json"
  }

  switch format {
  case "json":
    if err := json.Unmarshal(rawData, &data); err != nil {
      return nil, err
    }
  case "raw":
    strData := string(rawData)
    data["body"] = strData
    data["arg0"] = strings.TrimSpace(strData)
  case "split":
    if delimiter == "" {
      return nil, fmt.Errorf("split format requires a delimiter parameter")
    }
    parts := strings.Split(string(rawData), delimiter)
    for i, part := range parts {
      data[fmt.Sprintf("arg%d", i)] = strings.TrimSpace(part)
    }
  case "regex":
    if pattern == "" {
      return nil, fmt.Errorf("regex format requires a pattern")
    }
    re, err := regexp.Compile(pattern)
    if err != nil {
      return nil, err
    }

    matches := re.FindStringSubmatch(string(rawData))
    names := re.SubexpNames()

    if len(matches) > 0 {
      for i, match := range matches {
        if i == 0 {
          continue
        }
        name := names[i]
        if name != "" {
          data[name] = match
        } else {
          data[fmt.Sprintf("arg%d", i)] = match
        }
      }
    }
  case "conf":
    lines := strings.Split(string(rawData), "\n")
    section := ""
    for _, line := range lines {
      line = strings.TrimSpace(line)
      if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
        continue
      }
      
      if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
        section = line[1 : len(line)-1]
        if _, exists := data[section]; !exists {
          data[section] = make(map[string]interface{})
        }
        continue
      }
      
      parts := strings.SplitN(line, "=", 2)
      key := strings.TrimSpace(parts[0])
      val := "true"
      
      if len(parts) == 2 {
        val = strings.TrimSpace(parts[1])
        val = strings.Trim(val, `"'`)
      }
      
      if section != "" {
        if _, exists := data[section]; !exists {
          data[section] = make(map[string]interface{})
        }
        sectionMap := data[section].(map[string]interface{})
        sectionMap[key] = val
      } else {
        data[key] = val
      }
    }
  default:
    return nil, fmt.Errorf("unsupported format: %s", format)
  }

  return data, nil
}

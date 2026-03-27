package main

import (
  "fmt"
  "log"
  "maps"
  "strings"
  "strconv"
  "math"
  "regexp"
  "time"
  "encoding/json"
  "github.com/godbus/dbus/v5"
  "sync/atomic"
)

var run_index int64 
var templateRegex = regexp.MustCompile(`\{\{([^}]+)\}\}`)

func expandByTemplate(template string, vars_list ...map[string]interface{}) string {
  vars := make(map[string]interface{})
  for i := len(vars_list) - 1; i >= 0; i-- {
    maps.Copy(vars, vars_list[i])
  }

  return templateRegex.ReplaceAllStringFunc(template, func(match string) string {
    path := strings.TrimSpace(match[2 : len(match)-2])

    if val, ok := getNestedValue(vars, path); ok {
      return fmt.Sprintf("%v", val)
    }

    return "null"
  })
}

func NewRunID() string {
  newVal := atomic.AddInt64(&run_index, 1)
  return fmt.Sprintf("%d", newVal)
}

func matchFilters(filters map[string]interface{}, vars map[string]interface{}) bool {
  if len(filters) == 0 {
    return true
  }

  for key, expected := range filters {

    value, ok := vars[key]
    if !ok {
      return false
    }

    if s, ok := expected.(string); ok && s == "*" {
      continue
    }

    if fmt.Sprintf("%v", value) != fmt.Sprintf("%v", expected) {
      return false
    }
  }

  return true
}

func applyTransformations(transformations []Transformation, value_maps map[string]map[string]interface{}, vars_in map[string]interface{}, vars_out map[string]interface{}) bool {
  for _, t := range transformations {
    switch t.Type {
      case "copy":
        if res := ensureInOutVariables(t.In, t.Out, t.Optional, getValueByPreference(t.In, vars_out, vars_in)) ; res == 0 {
          log.Printf("applyTransformations copy: missing variables - ending transformations")
          return false
        } else if res == -1 {
          continue
        }

        val, ok := getNestedValue(vars_in, t.In)
        if ok {
          vars_out[t.Out] = val
        } else if !t.Optional {
          log.Printf("applyTransformations - copy: required input path '%s' not found", t.In)
          return false
        }

        log.Printf("applyTransformations - copy: %s = %v -> %s = %v", t.In, val, t.Out, vars_out[t.Out])
        
      case "template":
        if t.In == "" {
          if t.Optional {
            log.Printf("applyTransformations - optional input variable not found: %s", t.In)
            continue
          } else {
            log.Printf("applyTransformations - input variable not found: %s - ending transformations", t.In)
            return false
          }
        }
        if t.Out == "" {
          log.Printf("applyTransformations - no output specified: %s", t.Out)
          return false
        }

        vars_out[t.Out] = expandByTemplate(t.In, vars_out, vars_in)
        log.Printf("applyTransformations - template: %s -> %s = %v", t.In, t.Out, vars_out[t.Out])

      case "value_map":
        if res := ensureInOutVariables(t.In, t.Out, t.Optional, getValueByPreference(t.In, vars_out, vars_in)) ; res == 0 {
          log.Printf("applyTransformations value_map: missing variables - ending transformations")
          return false
        } else if res == -1 {
          continue
        }

        if t.Map == "" {
          log.Printf("applyTransformations - no value map specified: %s", t.Map)
          return false
        }

        if _, ok := value_maps[t.Map] ; !ok {
          log.Printf("applyTransformations - no value map found: %s", t.Map)
          return false
        }

        value_out, ok := applyValueMap(value_maps[t.Map], getValueByPreference(t.In, vars_out, vars_in))

        if !ok {
          log.Printf("applyTransformations - value_map failed: %v", value_out)
          return false
        }

        log.Printf("applyTransformations - value_map: %s = %v -> %s = %v", t.In, getValueByPreference(t.In, vars_out, vars_in), t.Out, value_out)
        vars_out[t.Out] = value_out

      case "math":
        if t.In == "" {
          if t.Optional {
            log.Printf("applyTransformations - optional input variable not found: %s", t.In)
            continue
          } else {
            log.Printf("applyTransformations - input variable not found: %s - ending transformations", t.In)
            return false
          }
        }
        if t.Out == "" {
          log.Printf("applyTransformations - no output specified: %s", t.Out)
          return false
        }

        expr := expandByTemplate(t.In, vars_out, vars_in)
        if (t.In == expr || expr == "") {
          if t.Optional {
            log.Printf("applyTransformations - optional input variables not found: %s", t.In)
            continue
          } else {
            log.Printf("applyTransformations - input variables not found: %s - ending transformations", t.In)
            return false
          }
        }

        value_out, err := evalMath(expr)
        if err != nil {
          if t.Optional {
            log.Printf("applyTransformations -  math expression error: %s", err)
            continue
          } else {
            log.Printf("applyTransformations -  math expression error: %s - ending transformations", err)
            return false
          }
        }

        log.Printf("applyTransformations - math: %s -> %s = %v", t.In, t.Out, value_out)
        vars_out[t.Out] = value_out

      case "round":
        if res := ensureInOutVariables(t.In, t.Out, t.Optional, getValueByPreference(t.In, vars_out, vars_in)) ; res == 0 {
          log.Printf("applyTransformations round: missing variables - ending transformations")
          return false
        } else if res == -1 {
          continue
        } 

        v := getValueByPreference(t.In, vars_out, vars_in)

        f, ok := toFloat64(v)
        if !ok {
          if t.Optional {
            log.Printf("applyTransformations round: value can't be cast to float: %v", v)
            continue
          } else {
            log.Printf("applyTransformations round: value can't be cast to float: %v - ending transformations", v)
            return false
          }
        }

        d := t.DecimalPlaces
        if d < 0 {
          d = 0
        }

        p := math.Pow(10, float64(d))
        vars_out[t.Out] = math.Round(f*p) / p

        log.Printf("applyTransformations - round: %s = %v -> %s = %v ", t.In, v, t.Out, vars_out[t.Out])

      case "format_date":
        val := getValueByPreference(t.In, vars_out, vars_in)
        if res := ensureInOutVariables(t.In, t.Out, t.Optional, val); res == 0 {
          log.Printf("applyTransformations date_format: missing variables - ending transformations")
          return false
        } else if res == -1 {
          continue
        }

        input, ok := val.(string)
        if !ok || input == "" {
          if t.Optional {
            continue
          }
          log.Printf("applyTransformations - date_format: input is not a valid string")
          return false
        }

        getLayout := func(alias string) string {
          switch strings.ToLower(alias) {
          case "rfc1123z", "email": return time.RFC1123Z
          case "rfc3339", "iso8601": return time.RFC3339
          case "ofono", "mobile": return "2006-01-02T15:04:05-0700"
          case "unix": return "unix"
          case "ansic": return time.ANSIC
          case "kitchen": return time.Kitchen
          case "stamp": return time.Stamp
          default: return alias
          }
        }

        inLayout := getLayout(t.InFormat)
        outLayout := getLayout(t.OutFormat)

        var timeVal time.Time
        var err error

        if inLayout == "unix" {
          i, parseErr := strconv.ParseInt(input, 10, 64)
          if parseErr == nil {
            timeVal = time.Unix(i, 0)
          } else {
            err = parseErr
          }
        } else {
          timeVal, err = time.Parse(inLayout, input)
        }

        if err != nil {
          if t.Optional {
            log.Printf("applyTransformations - date_format parse error: %v", err)
            continue
          }
          log.Printf("applyTransformations - date_format parse error: %v - ending transformations", err)
          return false
        }

        if outLayout == "unix" {
          vars_out[t.Out] = strconv.FormatInt(timeVal.Unix(), 10)
        } else {
          vars_out[t.Out] = timeVal.Format(outLayout)
        }

        log.Printf("applyTransformations - date_format: %s = %v -> %s = %v", t.In, input, t.Out, vars_out[t.Out])
    }
  }

  return true
}

func ensureInOutVariables(in string, out string, optional bool, in_value interface{}) int {
  success := true

  if in == "" {
    log.Printf("ensureInOutVariables - input variable name empty: %s, optional: %v", in, optional)
    success = false
  }

  if out == "" {
    log.Printf("ensureInOutVariables - output variable name empty: %s, optional: %v", out, optional)
    success = false
  }

  if in_value == nil {
    log.Printf("ensureInOutVariables - input variable not found: %s, optional: %v", in, optional)
    success = false
  }

  if !success && !optional {
    return 0
  }

  if !success {
    return -1
  }

  return 1
}

func getValueByPreference(variable string, values_list ...map[string]interface{}) interface{} {
  for _, values := range  values_list {
    if value, ok := getNestedValue(values, variable) ; ok {
      return value
    }
  }

  return nil
}

func applyValueMap(value_maps map[string]interface{}, value interface{}) (interface{}, bool) { 
  value_out, ok := value_maps[fmt.Sprintf("%v", value)]

  return value_out, ok
}

func topicMatches(filter, topic string) bool {
  f := strings.Split(filter, "/")
  t := strings.Split(topic, "/")

  for i := 0; i < len(f); i++ {
    if i >= len(t) {
      return false
    }

    switch f[i] {
    case "#":
      return true

    case "+":
      continue

    default:
      if f[i] != t[i] {
        return false
      }
    }
  }

  return len(t) == len(f)
}

func evalMath(s string) (float64, error) {
  tokens, err := tokenizeMath(s)
  if err != nil {
    return 0, err
  }

  rpn, err := toRPN(tokens)
  if err != nil {
    return 0, err
  }

  return evalRPN(rpn)
}

type mathToken struct {
  kind string
  val  string
}

func tokenizeMath(s string) ([]mathToken, error) {
  var out []mathToken

  i := 0
  for i < len(s) {

    c := s[i]

    if c == ' ' || c == '\t' {
      i++
      continue
    }

    if (c >= '0' && c <= '9') || c == '.' {
      start := i
      for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == '.') {
        i++
      }
      out = append(out, mathToken{"num", s[start:i]})
      continue
    }

    switch c {
    case '+', '-', '*', '/':
      out = append(out, mathToken{"op", string(c)})
    case '(':
      out = append(out, mathToken{"lp", "("})
    case ')':
      out = append(out, mathToken{"rp", ")"})
    default:
      return nil, fmt.Errorf("math: invalid character: %c", c)
    }

    i++
  }

  return out, nil
}

func prec(op string) int {
  if op == "+" || op == "-" {
    return 1
  }
  if op == "*" || op == "/" {
    return 2
  }
  return 0
}

func toRPN(tokens []mathToken) ([]mathToken, error) {
  var out []mathToken
  var stack []mathToken

  for _, t := range tokens {

    switch t.kind {

    case "num":
      out = append(out, t)

    case "op":
      for len(stack) > 0 {
        top := stack[len(stack)-1]
        if top.kind == "op" && prec(top.val) >= prec(t.val) {
          out = append(out, top)
          stack = stack[:len(stack)-1]
        } else {
          break
        }
      }
      stack = append(stack, t)

    case "lp":
      stack = append(stack, t)

    case "rp":
      found := false
      for len(stack) > 0 {
        top := stack[len(stack)-1]
        stack = stack[:len(stack)-1]

        if top.kind == "lp" {
          found = true
          break
        }

        out = append(out, top)
      }

      if !found {
        return nil, fmt.Errorf("math: mismatched parentheses")
      }
    }
  }

  for i := len(stack) - 1; i >= 0; i-- {
    if stack[i].kind == "lp" {
      return nil, fmt.Errorf("math: mismatched parentheses")
    }
    out = append(out, stack[i])
  }

  return out, nil
}

func evalRPN(tokens []mathToken) (float64, error) {
  var stack []float64

  for _, t := range tokens {

    if t.kind == "num" {
      v, err := strconv.ParseFloat(t.val, 64)
      if err != nil {
        return 0, err
      }
      stack = append(stack, v)
      continue
    }

    if t.kind == "op" {
      if len(stack) < 2 {
        return 0, fmt.Errorf("math: invalid expression")
      }

      b := stack[len(stack)-1]
      a := stack[len(stack)-2]
      stack = stack[:len(stack)-2]

      var r float64

      switch t.val {
      case "+":
        r = a + b
      case "-":
        r = a - b
      case "*":
        r = a * b
      case "/":
        r = a / b
      }

      stack = append(stack, r)
    }
  }

  if len(stack) != 1 {
    return 0, fmt.Errorf("math: invalid expression")
  }

  return stack[0], nil
}

func tfRound(in, out map[string]interface{}, t Transformation) error {
  v, ok := in[t.In]
  if !ok {
    return fmt.Errorf("round: missing input '%s'", t.In)
  }

  f, ok := toFloat64(v)
  if !ok {
    return fmt.Errorf("round: value '%s' is not numeric", t.In)
  }

  d := t.DecimalPlaces
  if d < 0 {
    d = 0
  }

  p := math.Pow(10, float64(d))
  out[t.Out] = math.Round(f*p) / p

  return nil
}

func toFloat64(v interface{}) (float64, bool) {
  switch x := v.(type) {
  case float64:
    return x, true
  case float32:
    return float64(x), true
  case int:
    return float64(x), true
  case int64:
    return float64(x), true
  case json.Number:
    f, err := x.Float64()
    return f, err == nil
  default:
    return 0, false
  }
}

func getNestedValue(data map[string]interface{}, path string) (interface{}, bool) {
  if path == "" {
    return nil, false
  }

  parts := strings.Split(path, "/")
  var current interface{} = data

  for _, part := range parts {
    if v, ok := current.(dbus.Variant); ok {
      current = v.Value()
    }

    switch c := current.(type) {
    case map[string]interface{}:
      val, ok := c[part]
      if !ok { return nil, false }
      current = val
    case map[string]dbus.Variant:
      val, ok := c[part]
      if !ok { return nil, false }
      current = val.Value()
    case map[string]string:
      val, ok := c[part]
      if !ok { return nil, false }
      current = val
    default:
      return nil, false
    }
  }

  if v, ok := current.(dbus.Variant); ok {
    return v.Value(), true
  }

  return current, true
}

func resolveActionParams(params map[string]interface{}, vars map[string]interface{}) (map[string]interface{}, bool) {
  out := make(map[string]interface{}, len(params))

  for k, raw := range params {
    s, isString := raw.(string)
    if !isString {
      out[k] = raw
      continue
    }

    if val, ok := getNestedValue(vars, s); ok {
      out[k] = val
      continue
    }

    if strings.Contains(s, "{{") {
      expanded := expandByTemplate(s, vars)
      if expanded == s {
        log.Printf("resolveActionParams - cannot expand: %s", s)
        return nil, false
      }
      out[k] = expanded
      continue
    }

    out[k] = s
  }

  return out, true
}

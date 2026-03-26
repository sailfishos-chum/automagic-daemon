package main

import (
  "fmt"
)

func (e *Engine) fetchSQL(runID string, ds DataSource) (map[string]interface{}, error) {
  db, err := e.getSQLConnection(ds.Type, ds.Address, ds.Username, ds.Password, ds.Path, ds.Database)
  if err != nil {
    return nil, err
  }

  query := ds.Query
  if (query == "") {
    query = ds.Pattern
  }

  rows, err := db.Query(ds.Query)
  if err != nil {
    return nil, err
  }
  defer rows.Close()

  columns, _ := rows.Columns()
  count := len(columns)
  values := make([]interface{}, count)
  valuePtrs := make([]interface{}, count)
  for i := range columns {
    valuePtrs[i] = &values[i]
  }

  data := make(map[string]interface{})
  if rows.Next() {
    if err := rows.Scan(valuePtrs...); err != nil {
      return nil, err
    }

    for i, col := range columns {
      val := values[i]
      if b, ok := val.([]byte); ok {
        data[col] = string(b)
      } else {
        data[col] = val
      }
    }
    return data, nil
  }

  return nil, fmt.Errorf("no rows found in %s", ds.Type)
}

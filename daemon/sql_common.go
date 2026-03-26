package main

import (
  "database/sql"
  "fmt"
  "sync"
  _ "modernc.org/sqlite"
  _ "github.com/go-sql-driver/mysql"
)

var (
  dbCache   sync.Map
  dbCacheMu sync.Mutex
)

func (e *Engine) getSQLConnection(pType, addr, user, pass, path, dbName string) (*sql.DB, error) {
  var driver, dsn string
  
  switch pType {
  case "sqlite":
    driver = "sqlite"
    dsn = path
  case "mysql":
    driver = "mysql"
    dsn = fmt.Sprintf("%s:%s@tcp(%s)/%s", user, pass, addr, dbName)
  default:
    return nil, fmt.Errorf("unsupported sql type: %s", pType)
  }

  if val, exists := dbCache.Load(dsn); exists {
    return val.(*sql.DB), nil
  }

  dbCacheMu.Lock()
  defer dbCacheMu.Unlock()

  if val, exists := dbCache.Load(dsn); exists {
    return val.(*sql.DB), nil
  }

  db, err := sql.Open(driver, dsn)
  if err != nil {
    return nil, err
  }
  
  dbCache.Store(dsn, db)
  return db, nil
}

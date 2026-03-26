package main

import (
  "crypto/tls"
  "fmt"
  "io"
  "log"
  "net"
  "strings"

  "github.com/emersion/go-imap"
  "github.com/emersion/go-imap/client"
  "github.com/emersion/go-message/mail"
)

func (e *Engine) fetchIMAP(runID string, ds DataSource) (map[string]interface{}, error) {
  address := ds.Address
  user := ds.Username
  pass := ds.Password
  mailbox := ds.Path

  if mailbox == "" {
    mailbox = "INBOX"
  }

  if address == "" || user == "" || pass == "" {
    maskedPass := strings.Repeat("*", len(pass))
    if len(pass) == 0 {
      maskedPass = "EMPTY"
    }
    err := fmt.Errorf("missing required params (address: %s, username: %s, password: %s)", address, user, maskedPass)
    log.Printf("[%s] fetchIMAP - %v", runID, err)
    return nil, err
  }

  host, port, err := net.SplitHostPort(address)
  if err != nil {
    host = address
    port = "993"
    address = net.JoinHostPort(host, port)
  }

  tlsConfig := &tls.Config{
    ServerName:         host,
    InsecureSkipVerify: ds.Insecure,
  }
  
  var c *client.Client

  if port == "993" {
    c, err = client.DialTLS(address, tlsConfig)
  } else {
    c, err = client.Dial(address)
    if err == nil {
      if ok, _ := c.SupportStartTLS(); ok {
        err = c.StartTLS(tlsConfig)
      }
    }
  }

  if err != nil {
    log.Printf("[%s] fetchIMAP - connect error: %v", runID, err)
    return nil, err
  }
  defer c.Logout()

  if err := c.Login(user, pass); err != nil {
    log.Printf("[%s] fetchIMAP - login error: %v", runID, err)
    return nil, err
  }

  _, err = c.Select(mailbox, false)
  if err != nil {
    log.Printf("[%s] fetchIMAP - mailbox error: %v", runID, err)
    return nil, err
  }

  criteria := imap.NewSearchCriteria()
  criteria.WithoutFlags = []string{imap.SeenFlag}
  uids, err := c.UidSearch(criteria)
  if err != nil {
    log.Printf("[%s] fetchIMAP - search error: %v", runID, err)
    return nil, err
  }

  if len(uids) == 0 {
    log.Printf("[%s] fetchIMAP - no unread messages", runID)
    return map[string]interface{}{"has_message": false}, nil
  }

  targetUID := uids[0]
  seqset := new(imap.SeqSet)
  seqset.AddNum(targetUID)

  section := &imap.BodySectionName{Peek: true}
  messages := make(chan *imap.Message, 1)
  done := make(chan error, 1)

  go func() {
    done <- c.UidFetch(seqset, []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid, section.FetchItem()}, messages)
  }()

  var msg *imap.Message
  for m := range messages {
    msg = m
  }

  if err := <-done; err != nil {
    log.Printf("[%s] fetchIMAP - fetch error: %v", runID, err)
    return nil, err
  }

  if msg == nil {
    return nil, fmt.Errorf("message vanished before fetch")
  }

  payload := map[string]interface{}{
    "has_message": true,
    "uid":         msg.Uid,
    "subject":     msg.Envelope.Subject,
    "date":        msg.Envelope.Date.Unix(),
    "message_id":  msg.Envelope.MessageId,
    "remaining":   len(uids) - 1,
  }

  if len(msg.Envelope.From) > 0 {
    payload["from"] = msg.Envelope.From[0].Address()
  }
  
  if len(msg.Envelope.To) > 0 {
    payload["to"] = msg.Envelope.To[0].Address()
  }

  var bodyData []byte
  r := msg.GetBody(section)
  if r != nil {
    format := ds.Format
    if format == "" {
      format = "raw"
    }

    mr, err := mail.CreateReader(r)
    if err == nil {
      for {
        p, err := mr.NextPart()
        if err != nil {
          break
        }
        
        var contentType string
        switch h := p.Header.(type) {
        case *mail.InlineHeader:
          contentType, _, _ = h.ContentType()
        case *mail.AttachmentHeader:
          contentType, _, _ = h.ContentType()
        }

        partData, _ := io.ReadAll(p.Body)

        if format != "none" && format != "null" && len(partData) > 0 {
          parsedBody, err := parsePayload(format, partData, ds.Pattern, ds.Delimiter)
          if err != nil {
            log.Printf("[%s] fetchIMAP - body parse error for %s: %v", runID, contentType, err)
          } else {
            parts := strings.Split(contentType, "/")
            if len(parts) == 2 {
              mainType := parts[0]
              subType := parts[1]

              if _, ok := payload[mainType]; !ok {
                payload[mainType] = make(map[string]interface{})
              }
              mainMap := payload[mainType].(map[string]interface{})

              if _, ok := mainMap[subType]; !ok {
                mainMap[subType] = make(map[string]interface{})
              }
              subMap := mainMap[subType].(map[string]interface{})

              for k, v := range parsedBody {
                subMap[k] = v
              }
            } else {
              if _, ok := payload[contentType]; !ok {
                payload[contentType] = make(map[string]interface{})
              }
              cmap := payload[contentType].(map[string]interface{})
              for k, v := range parsedBody {
                cmap[k] = v
              }
            }
          }
        }
      }
    } else {
      bodyData, _ = io.ReadAll(r)
      if format != "none" && format != "null" && len(bodyData) > 0 {
        parsedBody, err := parsePayload(format, bodyData, ds.Pattern, ds.Delimiter)
        if err != nil {
          log.Printf("[%s] fetchIMAP - body parse error: %v", runID, err)
        } else {
          for k, v := range parsedBody {
            payload[k] = v
          }
        }
      }
    }
  }

  log.Printf("[%s] fetchIMAP - grabbed 1 message (UID: %d), %d remaining in queue", runID, msg.Uid, len(uids)-1)
  return payload, nil
}

package clientmgr

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait = 1 * time.Second
	//pongWait  = 60 * time.Second
	pongWait = 5 * time.Second
	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = 1 * time.Second
)

// Message is the message sent by client.
type Message struct {
	Type    string                 `json:"type"`
	Content map[string]interface{} `json:"content"`
}

func connect(client *Client) {
	conn := client.conn
	defer func() {
		var ids []string
		conn.Close()
		client.Unregister()
		for repoID := range client.Repos {
			ids = append(ids, repoID)
		}
		client.Unsubscribe(ids)
	}()

	// Set keep alive.
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error { conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	go client.write()
	go client.keepAlive()

	for {
		var msg Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			if netErr, ok := err.(net.Error); ok {
				if netErr.Timeout() {
					log.Printf("client %s disconnected", conn.RemoteAddr())
					return
				}
			}

			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("failed to read json data from client: %v", err)
			}
			return
		}

		err = client.handleMessage(&msg)
		if err != nil {
			log.Printf("failed to parse message from client: %v", err)
			return
		}
	}
}

func (client *Client) handleMessage(msg *Message) error {
	var ids []string

	content := msg.Content
	repos, ok := content["repos"]
	if !ok {
		err := fmt.Errorf("recv unexpected content of %s message", msg.Type)
		return err
	}

	objs, ok := repos.([]interface{})
	if !ok {
		err := fmt.Errorf("failed to assert client content")
		return err
	}

	if msg.Type == "subscribe" {
		for _, obj := range objs {
			repo, ok := obj.(map[string]interface{})
			if !ok {
				continue
			}
			id, ok := repo["id"].(string)
			if !ok {
				continue
			}
			ids = append(ids, id)

		}
		client.Subscribe(ids)
	} else if msg.Type == "unsubscribe" {
		for _, obj := range objs {
			id, ok := obj.(string)
			if !ok {
				continue
			}
			ids = append(ids, id)

		}
		client.Unsubscribe(ids)
	} else {
		err := fmt.Errorf("recv unexpected type of message: %s", msg.Type)
		return err
	}

	return nil
}

func (client *Client) write() {
	for event := range client.WCh {
		err := client.conn.WriteJSON(event)
		if err != nil {
			log.Printf("failed to send notification to client: %v", err)
			continue
		}
	}
}

func (client *Client) keepAlive() {
	ticker := time.NewTicker(pingPeriod)
	for {
		<-ticker.C
		client.conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
			return
		}
	}
}

func (msg *Message) ParseEvent() *Event {
	if msg.Type != "repo-update" &&
		msg.Type != "file-lock-changed" &&
		msg.Type != "folder-perm-changed" {
		return nil
	}

	content := msg.Content
	repoID, ok := content["repo_id"].(string)
	if !ok {
		return nil
	}

	event := new(Event)
	event.RepoID = repoID
	event.Type = msg.Type
	event.Content = make(map[string]interface{})
	for k, v := range content {
		event.Content[k] = v
	}

	return event
}

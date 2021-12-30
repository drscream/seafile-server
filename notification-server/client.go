package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

const (
	writeWait = 1 * time.Second
	pongWait  = 5 * time.Second
	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = 1 * time.Second
)

// Message is the message sent by client.
type Message struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content"`
}

type SubList struct {
	Repos []Repo `json:"repos"`
}

type UnsubList struct {
	Repos []string `json:"repos"`
}

type Repo struct {
	RepoID string `json:"id"`
	Token  string `json:"jwt_token"`
}

// HandleMessages connects to the client to process message.
func (client *Client) HandleMessages() {
	go client.readMessages()
	go client.writeMessages()

	// Set keep alive.
	client.conn.SetPongHandler(func(string) error {
		client.Alive = time.Now()
		return nil
	})
	go client.keepAlive()
}

func (client *Client) readMessages() {
	conn := client.conn
	defer func() {
		var ids []string
		if !client.ConnClosed {
			conn.Close()
		}
		close(client.WCh)
		UnregisterClient(client)
		for repoID := range client.Repos {
			ids = append(ids, repoID)
		}
		for _, id := range ids {
			client.unsubscribe(id)
		}
	}()

	for {
		var msg Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			log.Debugf("failed to read json data from client: %s: %v", client.Addr, err)
			return
		}

		err = client.handleMessage(&msg)
		if err != nil {
			log.Info("%v", err)
			return
		}
	}
}

func (client *Client) handleMessage(msg *Message) error {
	content := msg.Content

	if msg.Type == "subscribe" {
		var list SubList
		err := json.Unmarshal(content, &list)
		if err != nil {
			return err
		}
		for _, repo := range list.Repos {
			client.subscribe(repo.RepoID)
		}
	} else if msg.Type == "unsubscribe" {
		var list UnsubList
		err := json.Unmarshal(content, &list)
		if err != nil {
			return err
		}
		for _, id := range list.Repos {
			client.unsubscribe(id)
		}
	} else {
		err := fmt.Errorf("recv unexpected type of message: %s", msg.Type)
		return err
	}

	return nil
}

// subscribe subscribes to notifications of repos.
func (client *Client) subscribe(repoID string) {
	client.Repos[repoID] = struct{}{}

	subMutex.Lock()
	subscribers, ok := subscriptions[repoID]
	if !ok {
		subscribers = newSubscribers(client)
	}
	subMutex.Unlock()

	subscribers.Mutex.Lock()
	subscribers.Clients[client.ID] = client
	subscribers.Mutex.Unlock()

	subscriptions[repoID] = subscribers

}

func (client *Client) unsubscribe(repoID string) {
	subMutex.Lock()

	subscribers, ok := subscriptions[repoID]
	if !ok {
		subMutex.Unlock()
		return
	}

	subscribers.Mutex.Lock()
	delete(subscribers.Clients, client.ID)
	subscribers.Mutex.Unlock()

	subMutex.Unlock()
}

func (client *Client) writeMessages() {
	for msg := range client.WCh {
		client.conn.SetWriteDeadline(time.Now().Add(writeWait))
		err := client.conn.WriteJSON(msg)
		if err != nil {
			log.Printf("failed to send notification to client: %v", err)
			client.ConnClosed = true
			client.conn.Close()
			return
		}
	}
}

func (client *Client) keepAlive() {
	ticker := time.NewTicker(pingPeriod)
	for {
		<-ticker.C
		if time.Since(client.Alive) > pongWait {
			client.ConnClosed = true
			client.conn.Close()
			return
		}
		client.conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
			client.ConnClosed = true
			client.conn.Close()
			return
		}
	}
}

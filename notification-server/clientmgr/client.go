package clientmgr

import (
	"sync"

	"github.com/gorilla/websocket"
)

const (
	chanBufSize = 10
)

// clients is a map from client id to Client structs.
// It contains all current connected clients. Each client is identified by 64-bit ID.
var clients map[uint64]*Client
var clientsMutex sync.RWMutex

// Use atomic operation to increase this value.
var nextClientID uint64 = 1

// subscriptions is a map from repo_id to Sbuscribers struct.
// It's protected by rw mutex.
var subscriptions map[string]*Subscribers
var subMutex sync.RWMutex

// Clicent contains information about a client.
// Two go routines are associated with each client to handle message reading and writting.
// Messages sent to the client have to be written into WCh, since only one go routine can write to a websocket connection.
type Client struct {
	// The ID of this client
	ID uint64
	// Websocket connection.
	conn *websocket.Conn
	// WCh is used to write messages to a client.
	// The structs written into the channel will be converted to JSON and sent to client.
	WCh chan interface{}
	// Repos is the repos this client subscribed to.
	Repos map[string]struct{}
}

// Subscribers contains the clients who subscribe to a repo's notifications.
type Subscribers struct {
	// Clients is a map from client id to Client struct, protected by rw mutex.
	Clients map[uint64]*Client
	Mutex   sync.RWMutex
}

// Init inits clients and subscriptions.
func Init() {
	clients = make(map[uint64]*Client)
	subscriptions = make(map[string]*Subscribers)
}

// NewClient creates a new client.
func NewClient(conn *websocket.Conn) *Client {
	client := new(Client)
	client.ID = nextClientID
	client.conn = conn
	client.WCh = make(chan interface{}, chanBufSize)
	client.Repos = make(map[string]struct{})

	return client
}

// Connect connects to the client to process message.
func (client *Client) Connect() {
	go connect(client)
}

// Register adds the client to the list of clients.
func (client *Client) Register() {
	clientsMutex.Lock()
	clients[client.ID] = client
	clientsMutex.Unlock()
}

// Unregister deletes the client from the list of clients.
func (client *Client) Unregister() {
	clientsMutex.Lock()
	delete(clients, client.ID)
	clientsMutex.Unlock()
}

// Subscribe subscribes to notifications of repos.
func (client *Client) Subscribe(ids []string) {
	for _, id := range ids {
		client.Repos[id] = struct{}{}
		client.subscribe(id)
	}
}

func (client *Client) subscribe(repoID string) {
	subMutex.Lock()

	subscribers, ok := subscriptions[repoID]
	if !ok {
		subscribers = newSubscribers(client)
	}
	subscribers.Mutex.Lock()
	subscribers.Clients[client.ID] = client
	subscribers.Mutex.Unlock()

	subscriptions[repoID] = subscribers

	subMutex.Unlock()
}

func newSubscribers(client *Client) *Subscribers {
	subscribers := new(Subscribers)
	subscribers.Clients = make(map[uint64]*Client)
	subscribers.Clients[client.ID] = client

	return subscribers
}

// Unsubscribe unsubscribes to notifications of repos.
func (client *Client) Unsubscribe(ids []string) {
	for _, id := range ids {
		client.unsubscribe(id)
	}
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

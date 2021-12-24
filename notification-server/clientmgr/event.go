package clientmgr

// Event is an event notification sent to the client
type Event struct {
	RepoID  string                 `json:"-"`
	Type    string                 `json:"type"`
	Content map[string]interface{} `json:"content"`
}

func (event *Event) Notify() {
	repoID := event.RepoID
	clients := make(map[uint64]*Client)

	subMutex.RLock()
	subscribers := subscriptions[repoID]
	if subscribers == nil {
		subMutex.RUnlock()
		return
	}
	subMutex.RUnlock()

	subscribers.Mutex.RLock()
	for clientID, client := range subscribers.Clients {
		clients[clientID] = client
	}
	subscribers.Mutex.RUnlock()

	go func() {
		// In order to avoid being blocked on a Client for a long time, it is necessary to write WCh in a non-blocking way,
		// and the waiting WCh needs to be blocked and processed after other Clients have finished writing.
		blockClients := make(map[uint64]*Client)
		for clientID, client := range clients {
			select {
			case client.WCh <- event:
			default:
				blockClients[clientID] = client
			}
		}

		for _, client := range blockClients {
			client.WCh <- event
		}
	}()
}

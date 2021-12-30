package main

import (
	"encoding/json"
	"reflect"

	log "github.com/sirupsen/logrus"
)

type RepoUpdateEvent struct {
	RepoID   string `json:"repo_id"`
	CommitID string `json:"commit_id"`
}

type FileLockEvent struct {
	RepoID      string `json:"repo_id"`
	Path        string `json:"path"`
	ChangeEvent string `json:"change_event"`
	LockUser    string `json:"lock_user"`
}

type FolderPermEvent struct {
	RepoID      string  `json:"repo_id"`
	Path        string  `json:"path"`
	Type        string  `json:"type"`
	ChangeEvent string  `json:"change_event"`
	User        string  `json:"user"`
	Group       float64 `json:"group"`
	Perm        string  `json:"perm"`
}

func Notify(msg *Message) {
	var repoID string

	content := msg.Content
	switch msg.Type {
	case "repo-update":
		var event RepoUpdateEvent
		err := json.Unmarshal(content, &event)
		if err != nil {
			log.Info(err)
			return
		}
		repoID = event.RepoID
	case "file-lock-changed":
		var event FileLockEvent
		err := json.Unmarshal(content, &event)
		if err != nil {
			log.Info(err)
			return
		}
		repoID = event.RepoID
	case "folder-perm-changed":
		var event FolderPermEvent
		err := json.Unmarshal(content, &event)
		if err != nil {
			log.Info(err)
			return
		}
		repoID = event.RepoID
	default:
		return
	}

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
		value := reflect.ValueOf(msg)
		var branches []reflect.SelectCase
		for _, client := range clients {
			branch := reflect.SelectCase{Dir: reflect.SelectSend, Chan: reflect.ValueOf(client.WCh), Send: value}
			branches = append(branches, branch)
		}

		for len(branches) != 0 {
			index, _, _ := reflect.Select(branches)
			branches = append(branches[:index], branches[index+1:]...)
		}
	}()
}

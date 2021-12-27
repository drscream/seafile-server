package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/haiwen/seafile-server/notification-server/clientmgr"
	"gopkg.in/ini.v1"
)

var configDir string
var logFile, absLogFile string
var host string
var port uint32

func init() {
	flag.StringVar(&configDir, "c", "", "config directory")
	flag.StringVar(&logFile, "l", "", "log file path")
}

func loadConfig() {
	notifyConfPath := filepath.Join(configDir, "notification.conf")

	config, err := ini.Load(notifyConfPath)
	if err != nil {
		log.Fatalf("Failed to load notification.conf: %v", err)
	}

	section, err := config.GetSection("general")
	if err != nil {
		log.Fatal("No general section in seafile.conf.")
	}

	host = "0.0.0.0"
	port = 8083
	if key, err := section.GetKey("host"); err == nil {
		host = key.String()
	}

	if key, err := section.GetKey("port"); err == nil {
		n, err := key.Uint()
		if err == nil {
			port = uint32(n)
		}
	}

}

func main() {
	flag.Parse()

	if configDir == "" {
		log.Fatal("config directory must be specified.")
	}

	_, err := os.Stat(configDir)
	if os.IsNotExist(err) {
		log.Fatalf("config directory %s doesn't exist: %v.", configDir, err)
	}

	if logFile == "" {
		absLogFile = filepath.Join(configDir, "notification.log")
		fp, err := os.OpenFile(absLogFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			log.Fatalf("Failed to open or create log file: %v", err)
		}
		log.SetOutput(fp)
	} else if logFile != "-" {
		absLogFile, err = filepath.Abs(logFile)
		if err != nil {
			log.Fatalf("Failed to convert log file path to absolute path: %v", err)
		}
		fp, err := os.OpenFile(absLogFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			log.Fatalf("Failed to open or create log file: %v", err)
		}
		log.SetOutput(fp)
	}

	// When logFile is "-", use default output (StdOut)
	log.SetFlags(log.Ldate | log.Ltime)

	loadConfig()

	clientmgr.Init()

	router := newHTTPRouter()

	log.Printf("notification server started.")

	addr := fmt.Sprintf("%s:%d", host, port)
	err = http.ListenAndServe(addr, router)
	if err != nil {
		log.Printf("notificationserver exiting: %v", err)
	}
}

func newHTTPRouter() *mux.Router {
	r := mux.NewRouter()
	r.Handle("/", appHandler(messageCB))
	r.Handle("/events/{slash:\\/?}", appHandler(eventCB))

	return r
}

func messageCB(rsp http.ResponseWriter, r *http.Request) *appError {
	upgrader := newUpgrader()
	conn, err := upgrader.Upgrade(rsp, r, nil)
	if err != nil {
		err := fmt.Errorf("failed to upgrade http to websocket: %v", err)
		return &appError{Error: err,
			Message: "",
			Code:    http.StatusInternalServerError,
		}
	}

	client := clientmgr.NewClient(conn)
	client.Register()
	client.Connect()

	return nil
}

func eventCB(rsp http.ResponseWriter, r *http.Request) *appError {
	msg := clientmgr.Message{}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return &appError{Error: err,
			Message: "",
			Code:    http.StatusInternalServerError,
		}
	}

	if err := json.Unmarshal(body, &msg); err != nil {
		return &appError{Error: err,
			Message: "",
			Code:    http.StatusInternalServerError,
		}
	}

	event := msg.ParseEvent()
	if event != nil {
		event.Notify()
	}

	return nil
}

func newUpgrader() *websocket.Upgrader {
	upgrader := &websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	return upgrader
}

type appError struct {
	Error   error
	Message string
	Code    int
}

type appHandler func(http.ResponseWriter, *http.Request) *appError

func (fn appHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e := fn(w, r)
	if e != nil {
		if e.Error != nil && e.Code == http.StatusInternalServerError {
			log.Printf("path %s internal server error: %v\n", r.URL.Path, e.Error)
		}
		http.Error(w, e.Message, e.Code)
	}
}

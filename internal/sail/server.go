package sail

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type SailServer struct {
	router *mux.Router
	rooms  map[RoomID]*Room
	mu     *sync.Mutex
}

func ProvideSailServer() SailServer {
	r := mux.NewRouter().UseEncodedPath()
	s := SailServer{
		router: r,
		rooms:  make(map[RoomID]*Room),
		mu:     &sync.Mutex{},
	}

	r.HandleFunc("/share", s.startRoom)
	r.HandleFunc("/join/{roomID}", s.joinRoom)
	r.HandleFunc("/view/{roomID}", s.viewRoom)

	return s
}

func (s SailServer) Router() http.Handler {
	return s.router
}

func (s SailServer) newRoom(conn *websocket.Conn) *Room {
	s.mu.Lock()
	defer s.mu.Unlock()

	room := NewRoom(conn)
	s.rooms[room.id] = room
	return room
}

func (s SailServer) addFanToRoom(ctx context.Context, roomID RoomID, conn *websocket.Conn) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	room, ok := s.rooms[roomID]
	if !ok {
		return fmt.Errorf("Room not found: %s", roomID)
	}

	room.AddFan(ctx, conn)
	return nil
}

func (s SailServer) closeRoom(room *Room) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.rooms, room.id)
	room.Close()
}

func (s SailServer) startRoom(w http.ResponseWriter, req *http.Request) {
	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error upgrading websocket: %v", err), http.StatusInternalServerError)
		return
	}

	room := s.newRoom(conn)
	err = room.ConsumeSource(req.Context())
	if err != nil {
		log.Printf("websocket closed: %v", err)
	}

	s.closeRoom(room)
}

func (s SailServer) joinRoom(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	roomID := RoomID(vars["roomID"])
	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error upgrading websocket: %v", err), http.StatusInternalServerError)
		return
	}

	err = s.addFanToRoom(req.Context(), roomID, conn)
	if err != nil {
		http.Error(w, fmt.Sprintf("Room add error: %v", err), http.StatusInternalServerError)
		return
	}
}

func (s SailServer) viewRoom(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	roomID := RoomID(vars["roomID"])

	// TODO(nick): Add room viewing
	_ = roomID
}
